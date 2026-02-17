run:
	go run main.go

build-proxy:
	GOOS=linux GOARCH=amd64  go build -o proxy ./cmd/proxy_server

build:
	GOOS=linux GOARCH=amd64  go build -o tool ./main.go
main:
	go mod tidy
	GOOS=linux GOARCH=amd64  go build -o tool ./main.go
	ssh wws 'mkdir -p ~/tools'
	ssh wws 'pgrep -ax tool || true'
	ssh wws 'pkill -x tool || true'
	ssh wws 'for i in 1 2 3 4 5; do pgrep -x tool >/dev/null || exit 0; sleep 1; done; pkill -9 -x tool || true'
	scp tool wws:~/tools/tool.new
	ssh wws 'mv ~/tools/tool.new ~/tools/tool'
	ssh -n -f wws 'cd ~/tools && nohup ./tool > tool.log 2>&1 < /dev/null &'
	ssh wws 'pgrep -ax tool || true'
