# UDP Emitter Client

## Dependencies
brew install go gcc

go get fyne.io/fyne/v2
go get google.golang.org/protobuf

## To run the go desktop client
- `go mod download`
- `go mod tidy`
- `go run main.go`
- `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`

## Setup Go
`go env GOPATH` to check where your `GOPATH` is
`export GOROOT=/usr/local/go`
`export GOPATH="$HOME/go"`
`export PATH=$PATH:$GOPATH/bin`

## Community use

RaceHub source contributions are available under the scope in [LICENSING.md](LICENSING.md).
Local features do not require a RaceHub subscription.
Third-party games, SDKs, and optional hosting keep their own terms and costs.
See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).
No hosted service or maintenance schedule is included.
