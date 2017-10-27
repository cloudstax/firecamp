
.PHONY: build docker test sources rpm

version=0.8.1

install:
	./scripts/install.sh

docker: install
	./scripts/builddocker.sh $(version) all

plugin:
	./scripts/builddocker.sh $(version) plugin

catalogs:
	./scripts/builddocker.sh $(version) catalogs

test:
	./scripts/gotest.sh

rpm: install
	mkdir -p build/SOURCES
	cp $(GOPATH)/bin/firecamp-dockervolume firecamp-dockervolume
	cp packaging/firecamp-dockervolume/rpm/firecamp-dockervolume.conf firecamp-dockervolume.conf
	tar -czf build/SOURCES/firecamp-dockervolume.tgz firecamp-dockervolume firecamp-dockervolume.conf
	rpmbuild --define '%_topdir $(PWD)/build' -bb packaging/firecamp-dockervolume/rpm/firecamp-dockervolume.spec
	rm firecamp-dockervolume
	rm firecamp-dockervolume.conf

clean:
	-rm -rf build
	-rm $(GOPATH)/bin/firecamp* || true
