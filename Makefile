SHELL := /bin/bash
APP := build/bin/CommitTree.app
NODE := source ~/.nvm/nvm.sh && nvm use 22 >/dev/null &&

ARCH ?= amd64

.PHONY: build build-linux dev icon icon-previews

build:
	$(NODE) ~/go/bin/wails build
	codesign --force --deep -s - $(APP)
	git checkout -- frontend/wailsjs/runtime 2>/dev/null || true

build-linux:
	docker buildx build --platform linux/$(ARCH) -f build/linux/Dockerfile \
		--output type=local,dest=build/bin/linux-$(ARCH) .
	tar -czf build/bin/CommitTree-linux-$(ARCH).tar.gz -C build/bin/linux-$(ARCH) CommitTree

dev:
	$(NODE) ~/go/bin/wails dev

icon:
	qlmanage -t -s 1024 -o build assets/icon.svg >/dev/null
	mv build/icon.svg.png build/appicon.png

icon-previews:
	mkdir -p build/icon-previews
	for f in assets/icon-variants/*.svg; do qlmanage -t -s 512 -o build/icon-previews "$$f" >/dev/null; done
