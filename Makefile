
.PHONY: install docker test lambda

org="cloudstax/"
version="0.9.3"

all: install

cli:
	cd syssvc/firecamp-service-cli; go install; cd -
	cd $(GOPATH)/bin; tar -zcf firecamp-service-cli.tgz firecamp-service-cli; cd -

install:
	./scripts/install.sh

lambda:
	./scripts/buildlambda.sh

docker: install
	./scripts/builddocker.sh $(org) $(version) all

pluginimages:
	./scripts/builddocker.sh $(org) $(version) pluginimages

manageimages:
	./scripts/builddocker.sh $(org) $(version) manageimages

catalogimages:
	./scripts/builddocker.sh $(org) $(version) catalogimages

test:
	./scripts/gotest.sh

clean:
	-rm $(GOPATH)/bin/firecamp* || true
	-rm -fr build || true

cleanall: clean
	-rm -fr $(GOPATH)/pkg/linux_amd64/github.com/cloudstax/firecamp
