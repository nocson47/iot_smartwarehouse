.PHONY: deps build docker-build compose-up compose-down compose-build up down test clean

# Use Docker Compose plugin if available ("docker compose"), fallback can be set by env
DOCKER_COMPOSE ?= docker compose

deps:
	go mod tidy

build:
	go build -o server .

docker-build:
	docker build -t iot-server:latest .


compose-build:
	$(DOCKER_COMPOSE) build


compose-up:
	$(DOCKER_COMPOSE) up -d --build


compose-down:
	$(DOCKER_COMPOSE) down

up: compose-up

down: compose-down

test: compose-up
	@echo "Waiting for services to become ready..."
	sleep 6
	# Publish a test message via server API which will publish to MQTT and should be saved
	curl -s -X POST -H 'Content-Type: application/json' -d '{"topic":"sensors/test-device","payload":"{\"device\":\"test-device\",\"temp\":22.5,\"hum\":55.0}"}' http://localhost:5000/api/publish || true
	sleep 2
	RES=$$(curl -s http://localhost:5000/api/readings?limit=5 | grep -c "test-device" || true)
	if [ "$$RES" -ge 1 ]; then \
		echo "[OK] integration test passed"; \
	else \
			echo "[FAILED] integration test failed - no reading found"; \
			$(DOCKER_COMPOSE) logs --tail=50; exit 1; \
	fi

clean:
	rm -f server
