# Strongboxs — tareas comunes
BIN_DIR ?= /usr/local/bin
APP_DIR ?= $(HOME)/.local/share/applications
PREFIX  ?=

.PHONY: build install uninstall server-up server-down server-logs test clean

## build: compila el binario del cliente
build:
	cd client && go build -o bin/strongboxs ./cmd/strongboxs

## install: instala binario + lanzador de escritorio (puede pedir sudo)
install: build
	install -Dm755 client/bin/strongboxs $(DESTDIR)$(BIN_DIR)/strongboxs
	install -Dm644 packaging/strongboxs.desktop \
		$(DESTDIR)$(APP_DIR)/strongboxs.desktop
	@echo "✓ strongboxs instalado en $(BIN_DIR)/strongboxs"
	@echo "  Config opcional: ~/.config/strongboxs/sync.env"

## uninstall: elimina binario y lanzador
uninstall:
	rm -f $(DESTDIR)$(BIN_DIR)/strongboxs
	rm -f $(DESTDIR)$(APP_DIR)/strongboxs.desktop

## server-up: levanta API + PostgreSQL dockerizados
server-up:
	docker compose up -d --build

## server-down: detiene la pila
server-down:
	docker compose down

## server-logs: sigue los logs de la API
server-logs:
	docker compose logs -f api

## server-venv: crea el entorno Python del servidor (para tests locales)
server-venv:
	python3 -m venv server/.venv
	server/.venv/bin/pip install --upgrade pip
	server/.venv/bin/pip install -r server/requirements.txt

## test: suite completa (Go + Python; usa server/.venv si existe)
PY := $(shell [ -x server/.venv/bin/pytest ] && echo ".venv/bin/pytest" || echo "pytest")
test:
	cd client && go test ./...
	cd server && $(PY) -q

## clean: limpia artefactos de compilación
clean:
	rm -rf client/bin
