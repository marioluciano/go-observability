.PHONY: help build up down logs clean test

help:
	@echo "Weather Observability Service - Makefile Commands"
	@echo ""
	@echo "  make build          Build Docker images"
	@echo "  make up             Start services with docker-compose"
	@echo "  make down           Stop and remove services"
	@echo "  make logs           View service logs"
	@echo "  make clean          Clean build artifacts and containers"
	@echo "  make test-valid     Test with valid CEP"
	@echo "  make test-invalid   Test with invalid CEP"
	@echo "  make test-notfound  Test with non-existent CEP"
	@echo ""

build:
	docker-compose build

up:
	docker-compose up -d
	@echo "Services started. Check them out:"
	@echo "  Service A: http://localhost:8080"
	@echo "  Service B: http://localhost:8081"
	@echo "  Zipkin:    http://localhost:9411"

down:
	docker-compose down

downn:
	docker-compose down -v

logs:
	docker-compose logs -f

logs-a:
	docker-compose logs -f service-a

logs-b:
	docker-compose logs -f service-b

logs-zipkin:
	docker-compose logs -f zipkin

clean: down
	docker system prune -f
	find . -name "service-a" -type f -delete
	find . -name "service-b" -type f -delete

test-valid:
	curl -X POST http://localhost:8080/weather \
		-H "Content-Type: application/json" \
		-d '{"cep":"01310100"}' | jq .

test-invalid:
	curl -X POST http://localhost:8080/weather \
		-H "Content-Type: application/json" \
		-d '{"cep":"123"}' | jq .

test-notfound:
	curl -X POST http://localhost:8080/weather \
		-H "Content-Type: application/json" \
		-d '{"cep":"99999999"}' | jq .

dev-a:
	cd service-a && go mod download && go run .

dev-b:
	cd service-b && go mod download && go run .