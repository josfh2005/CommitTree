package prompts

// pastDefaults holds the SHA-256 (of the trimmed text) of every default each
// prompt has shipped with before the registry (.defaults.json) existed. A
// user file matching one is an untouched copy EnsureFiles wrote, not a
// customization, so it must not shadow a newer default. Later defaults are
// recognised through the registry; this list does not grow.
var pastDefaults = map[string]bool{
	"4e3bda825a6aa22289750d4deeb1757a9f4ac100dc5378d643253144a011b818": true, // chat.md at 4b61d59
	"c781173fa2ed6fa6ad9d8f634628b74e166e42ab71e7338a5343f5582ccba32f": true, // chat.md at 0af8b1e
	"89bb568a513ad0b7df0898c5459a6c7cb4c48bd2bd1541fe43d58e348f4772ed": true, // chat.md at 4d41499
	"534c5d2e853b1ad893c1463a9df3f8a8079ea525b330f5235e3cf88d0f694a5d": true, // chat.md at 30dcb53
	"862861c630bf083472091f7659494ace14774a36c391b27fbb32cabb6d1e4b4f": true, // chat.md at f30c904
	"11dfd0e864cf35e1dfaf20b91758fe56efde44e78b8aafcba07f9e41b7a2ff0a": true, // chat.md at 4c0c55f
	"afa24921e2efcc9fa89df8bf049035efa02ee2cf3151ac002865efcec901be9e": true, // chat.md at 71bbd68
	"e75169950e1a88658d020a114cb3df144c0f9dc6041b1335b5545139f0ee07e0": true, // commit-message.md at 8e09b3e
	"e8fefe42caa3366cdd08ec2776f662b0ec41db64490db410f5dc3f7f8d5f7589": true, // explain-commit.md at 71bbd68
	"3be4d7462337d003e5fd709796863884f5fe309bd998db5a210d539a8fe8f0c7": true, // explain-lines.md at f261bfb
	"043c60f6ff68c06a81a9cb1e813cc4b1f4c439ffefe20537b8d4ac5b9472de17": true, // resolve-conflicts.md at f03184a
	"3b88a14b860dad6ed685b7a68261b69ed74af1738d450748d043666553b20499": true, // suggest-replies.md at ac915b9
	"a464cad73dc970ab0e2d577412ac01b9f3ad903703e7752dc29f53c3f31ba439": true, // suggest-replies.md at df7c87e
}
