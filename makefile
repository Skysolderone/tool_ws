run:
	go run main.go

build-proxy:
	GOOS=linux GOARCH=amd64  go build -o proxy ./cmd/proxy_server

build:
	GOOS=linux GOARCH=amd64  go build -o tool ./main.go
main:
	scp config.json wws:~/tools
	go mod tidy
	GOOS=linux GOARCH=amd64  go build -o tool ./main.go
	scp tool wws:~ 
	ssh  wws

