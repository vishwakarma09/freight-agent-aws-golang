.PHONY: build clean

build:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/api/bootstrap cmd/api/main.go
	cd bin/api && zip api.zip bootstrap
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/cron/bootstrap cmd/cron/main.go
	cd bin/cron && zip cron.zip bootstrap

clean:
	rm -rf bin
