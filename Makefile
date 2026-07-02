GIT_VERSION ?= v0.1.0

all: deps gen build build_ui

rebuild: gen build

build:
	@GO111MODULE=on go build -ldflags "-X github.com/goliac-project/goliac/internal/config.GoliacBuildVersion=${GIT_VERSION}" -o goliac ./cmd/goliac

build_ui:
	@echo "Building Goliac UI ..."
	@cd ./browser/goliac-ui/; corepack enable && yarn install --frozen-lockfile && yarn build

run_ui:
	@cd ./browser/goliac-ui/; corepack enable && yarn serve

test: deps verifiers
	@GO111MODULE=on go test -covermode=atomic -coverprofile=coverage.txt ./internal/...
	@go tool cover -html coverage.txt -o cover.html

gen: api_docs swagger

deps:
	@go install github.com/go-swagger/go-swagger/cmd/swagger@v0.33.2
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8

doc:
	npm run docs:build
	rm -rf ghpages
	rsync -av docs/.vitepress/dist/ ghpages

################################
### Private
################################

api_docs:
	@echo "Installing swagger-merger" && npm install swagger-merger -g
	@swagger-merger -i $(PWD)/swagger/index.yaml -o $(PWD)/docs/api_docs/bundle.yaml
	@echo "Installing redoc-cli" && npm install redoc-cli -g
	@redoc-cli bundle docs/api_docs/bundle.yaml -o docs/public/api_docs.html

verifiers: verify_lint verify_swagger

verify_lint:
	@echo "Running $@"
	@golangci-lint run -D errcheck ./internal/...

verify_swagger:
	@echo "Running $@"
	@swagger validate $(PWD)/docs/api_docs/bundle.yaml

swagger: verify_swagger
	@echo "Regenerate swagger files"
	@rm -f /tmp/configure_goliac.go
	@cp ./swagger_gen/restapi/configure_goliac.go /tmp/configure_goliac.go 2>/dev/null || :
	@rm -rf ./swagger_gen
	@mkdir ./swagger_gen
	@swagger generate server -t ./swagger_gen -f ./docs/api_docs/bundle.yaml
	@cp /tmp/configure_goliac.go ./swagger_gen/restapi/configure_goliac.go 2>/dev/null || :
