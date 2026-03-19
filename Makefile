generate:
	go generate ./...

package:
	docker build . --quiet --platform=linux/amd64 --tag runtime-amd64
	crossplane xpkg build \
    	--package-root=package \
    	--embed-runtime-image=runtime-amd64 \
    	--package-file=function-amd64.xpkg

push:
	up xpkg push \
  		disaster37/function-rancher2:v0.0.21 \
		-f ./function-amd64.xpkg

.PHONY: generate package push