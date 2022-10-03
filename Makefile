VERSION?="dev"

generate:
	go generate ./...

protobuf:
	bash scripts/protobufcheck.sh

fmtcheck:
	@sh -c "'$(CURDIR)/scripts/gofmtcheck.sh'"

docker: fmtcheck buildprep
	docker build --build-arg=VERSION=$(VERSION) .

buildprep:
	git fetch --tags -f
	mkdir -p dest
	$(eval VERSION=$(shell git describe --tags | cut -d "v" -f 2 | cut -d "-" -f 1))

.PHONY: fmtcheck generate protobuf docker
