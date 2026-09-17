HELPER := helpers/apple/.build/release/git-ui-apple

.PHONY: helper

helper:
	swift build -c release --package-path helpers/apple
