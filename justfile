set minimum-version := '1.53.0'
set default-list := true
set unstable
set lists

bin_folder := './bin'
main_package_path := './cmd/digen'
binary_name := 'digen'

# build the generator
build:
	go build -o='{{ bin_folder }}/{{ binary_name }}' {{ main_package_path }}

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

# list dependencies with available upgrades
upgradeable:
	go list -u -m -f '{{{{ if and .Update (not .Indirect) }}}}{{{{ . }}}}{{{{ end }}}}' all

# remove the bin folder
@clean:
	rm -rf {{ bin_folder }}
