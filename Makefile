
.PHONY: install docker test lambda

version=latest

all: docker lambda

cli:
	cd syssvc/firecamp-service-cli; go install; cd -
	cd $(GOPATH)/bin; tar -zcf firecamp-service-cli.tgz firecamp-service-cli; cd -

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
	-rm $(GOPATH)/bin/firecamp* || true
	-rm -fr build || true

cleanall: clean
	-rm -fr $(GOPATH)/pkg/linux_amd64/github.com/cloudstax/firecamp
