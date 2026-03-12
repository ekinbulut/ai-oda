.PHONY: dev worker swagger build clean crewai crewai-install

# API sunucusunu geliştirme modunda başlat
dev:
	go run ./cmd/api

# Worker'ı geliştirme modunda başlat (Redis Bridge ile)
worker:
	go run ./cmd/worker

# CrewAI Bridge'i geliştirme modunda başlat
crewai:
	cd crewai && python -m src.main

# CrewAI Python bağımlılıklarını yükle
crewai-install:
	cd crewai && pip install -e .
	@echo "✅ CrewAI bağımlılıkları yüklendi"

# Swagger dokümantasyonunu yeniden üret
swagger:
	~/go/bin/swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal
	@echo "✅ Swagger docs üretildi: http://localhost:8080/swagger/index.html"

# Swagger CLI'ı yükle (ilk kez)
swagger-install:
	go install github.com/swaggo/swag/cmd/swag@v1.16.4

# Tüm binary'leri derle
build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker

# Temizle
clean:
	rm -rf bin/
