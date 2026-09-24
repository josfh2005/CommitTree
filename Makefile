SHELL := /bin/bash
APP := build/bin/CommitTree.app
NODE := source ~/.nvm/nvm.sh && nvm use 22 >/dev/null &&

.PHONY: build dev icon icon-previews

build:
	$(NODE) ~/go/bin/wails build
	codesign --force --deep -s - $(APP)
	git checkout -- frontend/wailsjs/runtime 2>/dev/null || true

dev:
	$(NODE) ~/go/bin/wails dev

icon:
	qlmanage -t -s 1024 -o build assets/icon.svg >/dev/null
	mv build/icon.svg.png build/appicon.png

icon-previews:
	mkdir -p build/icon-previews
	for f in assets/icon-variants/*.svg; do qlmanage -t -s 512 -o build/icon-previews "$$f" >/dev/null; done
