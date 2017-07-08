
.PHONY: build docker test sources rpm

version=0.5-alpha

install:
	./scripts/install.sh

docker: install
	./scripts/builddocker.sh $(version)

test:
	./scripts/gotest.sh

rpm: install
	mkdir -p build/SOURCES
	cp $(GOPATH)/bin/openmanage-dockervolume openmanage-dockervolume
	cp packaging/openmanage-dockervolume/rpm/openmanage-dockervolume.conf openmanage-dockervolume.conf
	tar -czf build/SOURCES/openmanage-dockervolume.tgz openmanage-dockervolume openmanage-dockervolume.conf
	rpmbuild --define '%_topdir $(PWD)/build' -bb packaging/openmanage-dockervolume/rpm/openmanage-dockervolume.spec
	rm openmanage-dockervolume
	rm openmanage-dockervolume.conf

clean:
	-rm -rf build
	-rm $(GOPATH)/bin/openmanage*
