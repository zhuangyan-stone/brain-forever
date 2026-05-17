@echo off
chcp 65001 >nul
powershell -NoProfile -Command "&{[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); & '脑力在线.ps1'}"
pause
