.PHONY: test build fmt vet smoke docker-smoke ip2region-smoke
test:
	go test -race -cover ./...
build:
	go build -trimpath -o bin/cch-geoip ./cmd/cch-geoip
fmt:
	gofmt -w cmd internal
vet:
	go vet ./...
smoke: build
	go build -o bin/fixtures ./tools/fixtures
	python3 scripts/smoke.py
	python3 scripts/blacklist-smoke.py
docker-smoke: smoke
	CGO_ENABLED=0 go test -c -o bin/threat.test ./internal/threat
	CGO_ENABLED=0 go test -c -o bin/onlinegeo.test ./internal/onlinegeo
	python3 scripts/docker-smoke.py
	python3 scripts/ip2region-smoke.py
ip2region-smoke:
	python3 scripts/ip2region-smoke.py
