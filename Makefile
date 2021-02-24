VERSION?="0.0.1"

generate:
	go generate ./...

protobuf:
	bash scripts/protobufcheck.sh

fmtcheck:
	@sh -c "'$(CURDIR)/scripts/gofmtcheck.sh'"

docker: fmtcheck
	docker build .

.PHONY: fmtcheck generate protobuf docker
