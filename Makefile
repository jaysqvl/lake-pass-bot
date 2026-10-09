NPM ?= npm

.PHONY: frontend build

frontend:
	cd web/frontend && $(NPM) ci && $(NPM) run build

build: frontend
	mkdir -p bin
	go build -trimpath -o bin/lake-pass-bot ./cmd/lake-pass-bot
