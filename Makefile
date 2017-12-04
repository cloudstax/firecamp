
.PHONY: install docker test lambda

version=latest

install:
	./scripts/install.sh

lambda:
	./scripts/buildlambda.sh

docker: install
	./scripts/builddocker.sh $(version) all

pluginimages:
	./scripts/builddocker.sh $(version) pluginimages

manageimages:
	./scripts/builddocker.sh $(version) manageimages

catalogimages:
	./scripts/builddocker.sh $(version) catalogimages

test:
	./scripts/gotest.sh

clean:
	-rm -rf build
	-rm $(GOPATH)/bin/firecamp* || true
