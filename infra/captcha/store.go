package captcha

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
)

// ISession is a cache storage interface for captcha session data.
type ISession interface {
	Get(key string) interface{}
	Set(key string, value interface{})
	Delete(key string)
}

// Global captcha recognition singleton

var (
	theCaptchaRWMutex   = new(sync.RWMutex)
	theCaptchaTable     map[string]string // filename -> captcha code
	theCaptchaTableKeys []string          // filenames
)

func readCaptchaIndies(fn string) (map[string]string, []string, error) {
	fileIndex, err := os.Open(fn)
	if err != nil {
		return nil, nil, err
	}

	defer func() { _ = fileIndex.Close() }()

	m := map[string]string{}
	var keys []string

	lineScanner := bufio.NewScanner(fileIndex)

	for lineScanner.Scan() {
		line := lineScanner.Text()

		// split by tab
		if line != "" {
			kv := strings.Split(line, "\t")
			if len(kv) == 2 {
				m[kv[0]] = kv[1]
				keys = append(keys, kv[0])
			}
		}
	}

	if err := lineScanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("failed to scan captcha index file: %w", err)
	}

	return m, keys, nil
}

// Refresh reloads the captcha table from the given directory.
func Refresh(dir string) (count int, err error) {
	fn := dir + "index.dat"

	if maps, keys, e := readCaptchaIndies(fn); e != nil {
		err = fmt.Errorf("failed to read captcha index data: %w", e)
		return
	} else {
		count = len(keys)

		theCaptchaRWMutex.Lock()
		defer theCaptchaRWMutex.Unlock()

		theCaptchaTable = maps
		theCaptchaTableKeys = keys
	}

	return
}

// GetOne returns a random captcha entry.
func GetOne() (sn, code string) {
	theCaptchaRWMutex.RLock() // read lock
	defer theCaptchaRWMutex.RUnlock()

	if theCaptchaTable == nil {
		return "", ""
	}

	count := len(theCaptchaTableKeys)

	if count == 0 {
		return "", ""
	}

	// pick a random entry:
	randIndex := rand.Intn(count)
	sn = theCaptchaTableKeys[randIndex]

	var ok bool
	if code, ok = theCaptchaTable[sn]; !ok {
		return sn, ""
	} else {
		return sn, code
	}
}

// GetOneDifferent returns a random captcha entry that is different from the given code.
// It tries up to maxAttempts times; if all attempts fail (e.g. only one captcha available),
// it returns whatever GetOne() gives.
func GetOneDifferent(avoidCode string, maxAttempts int) (sn, code string) {
	for i := 0; i < maxAttempts; i++ {
		sn, code = GetOne()
		if sn == "" || code == "" {
			return "", ""
		}
		if code != avoidCode {
			return sn, code
		}
	}
	// Fallback: return the last one anyway
	return GetOne()
}

// makeCaptchaCacheKey builds a cache key for a captcha code.
// The key only depends on the action, because a session only needs one
// captcha code for a given action at any time.
func makeCaptchaCacheKey(action string) string {
	return fmt.Sprintf("CAPTCHA.%s", action)
}

// removeCaptchaCache deletes the cached captcha code.
func removeCaptchaCache(session ISession, action string) {
	key := makeCaptchaCacheKey(action)
	session.Delete(key)
}

// getCaptchaCache retrieves the cached captcha code for the given action.
func getCaptchaCache(session ISession, action string) string {
	key := makeCaptchaCacheKey(action)

	if di := session.Get(key); di == nil {
		return ""
	} else if code, ok := di.(string); !ok {
		return ""
	} else {
		return code
	}
}

// SetCaptchaCache stores a captcha code for the given action.
func SetCaptchaCache(session ISession, action, code string) {
	key := makeCaptchaCacheKey(action)
	session.Set(key, code)
}

// VerifyCaptchaCacheEx verifies a captcha code and distinguishes between
// "expired" (no captcha set) and "wrong" (code doesn't match).
// Returns:
//   - exists: true if a captcha was previously stored for this action
//   - matches: true if the provided code matches the stored captcha
func VerifyCaptchaCacheEx(session ISession, action, code string) (exists, matches bool) {
	codeInCache := getCaptchaCache(session, action)

	if codeInCache == "" {
		return false, false // never set or already consumed → expired
	}

	if !strings.EqualFold(codeInCache, code) {
		return true, false // exists but wrong
	}

	removeCaptchaCache(session, action) // consume on success
	return true, true
}

/*
上面的代码，除包名不变包，全删除，我们要完全重新实现 captcha （提供者）的逻辑，改用 OOP 方式，类型为 CaptchaProvider

CaptchaProvider 支持从 d1, d2 两个目录，读取图形验证码及其答案，类似

type CaptchaData struct {
	QCn string // 要显示在前端中的中文提问
	QEn string // 提问的英文版

	Left, Top, Right, Bottom int // 用户点击图片此位置，为验证通过
}

type CaptchaItem struct {
	Image string // 验证码图片，相对 www 根路径，方便在HTML中展现。
	Data : CaptchaData // 验证码数据，含问题和答案。
}


构造 CaptchaProvider 对象，需提供以下入参：

1. captchaURLBase string ，指明存储验证码图片与数据的网站 URL。实际目录中，它将包含 d1 和 d2  两个目录，以及 dx.active 文件，x 为1或2。

2. captchaDirBase string , 服务器上，存储验证码图片与数据的路径 ，和 captchaURLBase 实质指向同一目录，只是后者用于前端 URL 访问，而captchaDirBase，用于服务程序在本机读取。

3.1 启动时，CaptchaProvider 会将 captchaDirBase / d1 / png 下的文件名（只需要文件名，不需要路径及扩展名，文件只选 .png ）全部加载，同时将 captchaDirBase / d1 / json 下的 *.json ，全部读入；然后 遍历 *.png ，检查是否有对应的 *.json，不存在的直接跳过；最终得到一个 map[key] value .
其中 key 是 图片名字（如前所述，无扩展名，无路径）。value是读入的  json 文件数据 [] byte。然后通过 Save(redis,  map, key) 方法，存入会话的redis缓存redis，并且以 redis 哈希表存储 ，在 captcha 包内，它只定义成接口。 key 是 CAPTCHAS_store.d1 。

3.2 完全一致的方法，读取 captchaDirBase / d2 / 下的 png 和 json 子目录的数据，只是最终存入 redis 的哈希表数据的 key 是 CAPTCHAS_store.d2。

（注意，3.1 和 3.2 要共用一个内部方法，由入参区分 d1或d2）

4. 查看 captchaDirBase 下，是否有 d1.active。如果有 d1.active，设置本身字段 ActiveDir 为 d1，否则检查是否有 d2.active，有设置 activeDir 为 d2。 如果二者都没有，默认认为活动目录为 d1。

5. 第 3 步中，map数据整体写入 redis 后，就可以在程序内存中抛弃。

6. 提供 GetOne（）方法。通过 activeDir 字段组装出 CAPTCHA_dx （x为1或2），然后从 redis 中随机（HRANDFIELD）取一个字段(key，即验证码图片名字)，和 activeDir 以及 png 组装成 URL 相对路径 ，返回，比如 "d1/png/sfsfsfsfsdfsdfsdfsd.png"。 其中 sfsfsfsfsdfsdfsdfsd 来自 key；同时还要返回 该 key 对的值（JSON格式）。

oneCaptcha = GetOne() 作用：调用通过该方法，即可从当前活动的目录下，取出一个随机的验证码（及其问题/答案数据）。

7. 提供 Refresh (activeDir string) 方法。依据 activeDir是 d1 或 d2 ，执行 3.1 或 3.2  。并且更新 activeDir 字段为传入的同名参数。本操作中，不管入参 activeDir 和 字段 activeDir 是否一致，都要执行。刷新之前，应全清 redis 中的原有数据。

（注意，GetOne 和 Refresh () 可能存在数据访问冲突，需考虑加锁。不过不同GetOne()之间应无需加锁，所以这里可能用的是读写锁。 ）

8. 说一下调用方：调用 GetOne，源于前端使用短信登录（含注册），重置密码需要图形校验码时的 api 请求。当前其 handler 现在被错误地实现在：infra/captcha/handler.go 内，这大错特错，应该实现到 internal/user 内，比如其下的 login.go 。调用方 通过 GetOne() 得到的 oneCaptcha ，又会被存入时效通常是 2 分钟 的 redis 缓存，类似当前本文件实现的 SetCaptchaCache(action)，对应的key，除 action 以外，还应有当前会话 SN（避免取得到的会话的 key，组合方式类似 ：sessionid::captcha::action ），只是该函数同样需要到 user 登录 模块中去（internal/user/login.go）。

9. 用户发送登录短信时，之前前端通过 api 传来的是用户识别后输入的验证文字。现在的验证操作是，用户击验证图片某个位置，前端将鼠标在图片上的相对坐标，经过处理后（处理方法见下），和 图片校验码名字，传到 服务端，后者从 redis 中取回 oneCaptcha ，对比文件名是否一致，以及更重要的：点击位置是否正确。

10。由于后端记录的是图片原始尺寸的位置（矩形左上与右下坐标），但前端图片展现可能被缩放（但肯定是等比例缩放的），所以鼠标位置，还需要前端利用js，取得图片DOM展现长宽，以及图片的实际长宽，然后实现坐标映射（等比例放大或缩小）。

（注：当前图片大小为 600 * 300 ,即 2 : 1关系。所以前端的登录过程，也要将验证码显示，调整为此比例，建议是 480 * 240）。

11.小结：本质上，只是图形验证码的“标准答案不一样”以及用户回答问题的方式不一样以外，整个验证逻辑流程是一致的。总是将 CaptchaProvider 实现对了。

另，Refresh() 需实现。但关于它的调用（以及对外路由），我们在下一个计划再实现，

*/
