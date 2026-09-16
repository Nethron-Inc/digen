set minimum-version := '1.52.0'
set default-list := true

main_package_path := './cmd/digen'

# install the generator into GOBIN
install:
	go install {{ main_package_path }}

# vet the module
vet:
	go vet ./...

# regenerate the example's container
generate-example:
	go run {{ main_package_path }} -pkg ./examples/basic

# run the example
run-example: generate-example
	go run ./examples/basic

# tidy the modfile and format the code
tidy:
	go mod tidy
	go fmt ./...

# interactively upgrade dependencies
upgrade:
	go run github.com/oligot/go-mod-upgrade@latest
	go mod tidy
