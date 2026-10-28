# Начните здесь

Проект написан на Go. Архив содержит только небольшой исходный код и примеры.

1. Установите Go с https://go.dev/dl/.
2. Распакуйте архив и откройте PowerShell в папке labelqc-go.
3. Проверьте проект:

```powershell
go test ./...
go run ./cmd/labelqc demo --out local/first-demo
```

Откройте local/first-demo/REPORT.md. Это синтетическая демонстрация, реальные LLM-запросы не выполняются.

Для подключения Label Studio, LLM-разметки и Judge следуйте README.md.
Готовый пример отчёта уже есть в reports/demo/REPORT.md.
