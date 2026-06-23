package agent

import (
	"BrainForever/infra/embedder"
	"BrainForever/internal/local/agent/toolimp"
	"BrainForever/internal/local/store"
	"context"
)

// traitSearchAdapter adapts searcher.WebSearcher to implement the toolimp.TraitSearcher interface
type traitSearchAdapter struct {
	client embedder.Embedder
	store  *store.VectorStore
}

// SearchByText 通过指定的文本，查找匹配的个人特征描述
func (a *traitSearchAdapter) SearchByText(ctx context.Context, queryText string, category int, topK int) ([]toolimp.TraitSource, error) {
	// 1. 使用 client (embedder.Embedeer) 计算出 queryText 的 vector
	// 2. 调用 store 的
}

func (a *traitSearchAdapter) SearchByKeyword(ctx context.Context, queryKeyword string, queryType int) ([]toolimp.TraitSource, error) {

}

// Close releases any underlying resources held by the searcher.
func (a *traitSearchAdapter) Close() error {

}
