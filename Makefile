SHELL := /bin/bash
HELPER := helpers/apple/.build/release/git-ui-apple
APP := build/bin/git-ui.app
NODE := source ~/.nvm/nvm.sh && nvm use 22 >/dev/null &&

.PHONY: helper build dev icon icon-previews

helper:
	swift build -c release --package-path helpers/apple

build: helper
	$(NODE) ~/go/bin/wails build
	cp $(HELPER) $(APP)/Contents/MacOS/git-ui-apple
	codesign --force --deep -s - $(APP)
	git checkout -- frontend/wailsjs/runtime 2>/dev/null || true

dev: helper
	$(NODE) ~/go/bin/wails dev

icon:
	qlmanage -t -s 1024 -o build assets/icon.svg >/dev/null
	mv build/icon.svg.png build/appicon.png

icon-previews:
	mkdir -p build/icon-previews
	for f in assets/icon-variants/*.svg; do qlmanage -t -s 512 -o build/icon-previews "$$f" >/dev/null; done
