
.PHONY: build docker test sources rpm

version=0.9

install:
	./scripts/install.sh

lambda:
	./scripts/buildlambda.sh

docker: install
	./scripts/builddocker.sh $(version) all

plugin:
	./scripts/builddocker.sh $(version) plugin

catalogs:
	./scripts/builddocker.sh $(version) catalogs

test:
	./scripts/gotest.sh

clean:
	-rm -rf build
	-rm $(GOPATH)/bin/firecamp* || true
