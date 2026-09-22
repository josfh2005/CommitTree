export namespace ai {
	
	export class ToolCall {
	    id: string;
	    name: string;
	    args: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new ToolCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.args = source["args"];
	    }
	}
	export class Message {
	    role: string;
	    content: string;
	    toolCalls?: ToolCall[];
	    toolName?: string;
	    stopped?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	        this.toolCalls = this.convertValues(source["toolCalls"], ToolCall);
	        this.toolName = source["toolName"];
	        this.stopped = source["stopped"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace app {
	
	export class ProviderStatus {
	    provider: string;
	    hasKey: boolean;
	    keyHint: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.hasKey = source["hasKey"];
	        this.keyHint = source["keyHint"];
	        this.error = source["error"];
	    }
	}
	export class OllamaStatus {
	    running: boolean;
	    url: string;
	    chatModel: string;
	    models: ollama.Model[];
	    chatModelInstalled: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new OllamaStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.url = source["url"];
	        this.chatModel = source["chatModel"];
	        this.models = this.convertValues(source["models"], ollama.Model);
	        this.chatModelInstalled = source["chatModelInstalled"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AIStatus {
	    ollama: OllamaStatus;
	    providers: ProviderStatus[];
	    keyStore?: string;
	
	    static createFrom(source: any = {}) {
	        return new AIStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ollama = this.convertValues(source["ollama"], OllamaStatus);
	        this.providers = this.convertValues(source["providers"], ProviderStatus);
	        this.keyStore = source["keyStore"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConflictFile {
	    path: string;
	    resolved: boolean;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new ConflictFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.resolved = source["resolved"];
	        this.text = source["text"];
	    }
	}
	export class LogRow {
	    hash: string;
	    short: string;
	    parents: string[];
	    author: string;
	    email: string;
	    // Go type: time
	    date: any;
	    subject: string;
	    refs: gitlog.Ref[];
	    lane: number;
	    color: number;
	    edges: graph.Edge[];
	    isMerge: boolean;
	    isHead: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LogRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.short = source["short"];
	        this.parents = source["parents"];
	        this.author = source["author"];
	        this.email = source["email"];
	        this.date = this.convertValues(source["date"], null);
	        this.subject = source["subject"];
	        this.refs = this.convertValues(source["refs"], gitlog.Ref);
	        this.lane = source["lane"];
	        this.color = source["color"];
	        this.edges = this.convertValues(source["edges"], graph.Edge);
	        this.isMerge = source["isMerge"];
	        this.isHead = source["isHead"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LogPage {
	    rows: LogRow[];
	    hasMore: boolean;
	    graphVisible: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LogPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = this.convertValues(source["rows"], LogRow);
	        this.hasMore = source["hasMore"];
	        this.graphVisible = source["graphVisible"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class RepoItem {
	    id: string;
	    name: string;
	    path: string;
	    missing: boolean;
	    group?: string;
	    branch: string;
	
	    static createFrom(source: any = {}) {
	        return new RepoItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.missing = source["missing"];
	        this.group = source["group"];
	        this.branch = source["branch"];
	    }
	}

}

export namespace gitlog {
	
	export class FileChange {
	    status: string;
	    path: string;
	    oldPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	    }
	}
	export class Ref {
	    name: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new Ref(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	    }
	}
	export class Details {
	    hash: string;
	    short: string;
	    parents: string[];
	    author: string;
	    email: string;
	    // Go type: time
	    date: any;
	    subject: string;
	    refs: Ref[];
	    body: string;
	    committer: string;
	    committerEmail: string;
	    // Go type: time
	    commitDate: any;
	    files: FileChange[];
	
	    static createFrom(source: any = {}) {
	        return new Details(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.short = source["short"];
	        this.parents = source["parents"];
	        this.author = source["author"];
	        this.email = source["email"];
	        this.date = this.convertValues(source["date"], null);
	        this.subject = source["subject"];
	        this.refs = this.convertValues(source["refs"], Ref);
	        this.body = source["body"];
	        this.committer = source["committer"];
	        this.committerEmail = source["committerEmail"];
	        this.commitDate = this.convertValues(source["commitDate"], null);
	        this.files = this.convertValues(source["files"], FileChange);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Filters {
	    text: string;
	    branch: string;
	    author: string;
	    since: string;
	    until: string;
	    paths: string[];
	
	    static createFrom(source: any = {}) {
	        return new Filters(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.branch = source["branch"];
	        this.author = source["author"];
	        this.since = source["since"];
	        this.until = source["until"];
	        this.paths = source["paths"];
	    }
	}

}

export namespace gitsettings {
	
	export class Settings {
	    pullStrategy: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pullStrategy = source["pullStrategy"];
	    }
	}

}

export namespace graph {
	
	export class Edge {
	    from: number;
	    to: number;
	    color: number;
	    kind: number;
	    target?: string;
	
	    static createFrom(source: any = {}) {
	        return new Edge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = source["from"];
	        this.to = source["to"];
	        this.color = source["color"];
	        this.kind = source["kind"];
	        this.target = source["target"];
	    }
	}

}

export namespace merge {
	
	export class Result {
	    outcome: number;
	    conflicts: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.conflicts = source["conflicts"];
	    }
	}
	export class State {
	    kind: string;
	    merging: boolean;
	    from: string;
	    into: string;
	    conflicts: string[];
	    manual: string[];
	    staged: string[];
	    unstaged: string[];
	    step?: number;
	    total?: number;
	    subject?: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.merging = source["merging"];
	        this.from = source["from"];
	        this.into = source["into"];
	        this.conflicts = source["conflicts"];
	        this.manual = source["manual"];
	        this.staged = source["staged"];
	        this.unstaged = source["unstaged"];
	        this.step = source["step"];
	        this.total = source["total"];
	        this.subject = source["subject"];
	    }
	}

}

export namespace ollama {
	
	export class Model {
	    name: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new Model(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	    }
	}

}

export namespace ops {
	
	export class AheadBehind {
	    ahead: number;
	    behind: number;
	
	    static createFrom(source: any = {}) {
	        return new AheadBehind(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	    }
	}
	export class ResetInfo {
	    undone: number;
	    gained: number;
	    pushed: number;
	    upstream: string;
	
	    static createFrom(source: any = {}) {
	        return new ResetInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.undone = source["undone"];
	        this.gained = source["gained"];
	        this.pushed = source["pushed"];
	        this.upstream = source["upstream"];
	    }
	}
	export class Result {
	    outcome: number;
	    conflicts: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.conflicts = source["conflicts"];
	    }
	}

}

export namespace prompts {
	
	export class Info {
	    name: string;
	    customized: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.customized = source["customized"];
	    }
	}

}

export namespace refs {
	
	export class Branch {
	    name: string;
	    remote: string;
	    hash: string;
	    current: boolean;
	    upstream: string;
	
	    static createFrom(source: any = {}) {
	        return new Branch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.remote = source["remote"];
	        this.hash = source["hash"];
	        this.current = source["current"];
	        this.upstream = source["upstream"];
	    }
	}
	export class Tag {
	    name: string;
	    hash: string;
	
	    static createFrom(source: any = {}) {
	        return new Tag(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.hash = source["hash"];
	    }
	}
	export class Remote {
	    name: string;
	    branches: Branch[];
	
	    static createFrom(source: any = {}) {
	        return new Remote(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.branches = this.convertValues(source["branches"], Branch);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Refs {
	    head: string;
	    headHash: string;
	    detached: boolean;
	    local: Branch[];
	    remotes: Remote[];
	    tags: Tag[];
	
	    static createFrom(source: any = {}) {
	        return new Refs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.head = source["head"];
	        this.headHash = source["headHash"];
	        this.detached = source["detached"];
	        this.local = this.convertValues(source["local"], Branch);
	        this.remotes = this.convertValues(source["remotes"], Remote);
	        this.tags = this.convertValues(source["tags"], Tag);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

export namespace repos {
	
	export class Repo {
	    id: string;
	    name: string;
	    path: string;
	    missing: boolean;
	    group?: string;
	
	    static createFrom(source: any = {}) {
	        return new Repo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.missing = source["missing"];
	        this.group = source["group"];
	    }
	}

}

export namespace settings {
	
	export class Settings {
	    ollamaURL: string;
	    chatProvider: string;
	    chatModel: string;
	    taskProvider: string;
	    taskModel: string;
	    commitMessage: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ollamaURL = source["ollamaURL"];
	        this.chatProvider = source["chatProvider"];
	        this.chatModel = source["chatModel"];
	        this.taskProvider = source["taskProvider"];
	        this.taskModel = source["taskModel"];
	        this.commitMessage = source["commitMessage"];
	    }
	}

}

export namespace stash {
	
	export class Entry {
	    index: number;
	    message: string;
	    branch: string;
	    hash: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.message = source["message"];
	        this.branch = source["branch"];
	        this.hash = source["hash"];
	    }
	}
	export class File {
	    path: string;
	    oldPath?: string;
	    status: string;
	    untracked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.status = source["status"];
	        this.untracked = source["untracked"];
	    }
	}

}

export namespace worktree {
	
	export class CommitInfo {
	    stagedCount: number;
	    canAmend: boolean;
	    lastMessage: string;
	    pushed: boolean;
	    upstream: string;
	
	    static createFrom(source: any = {}) {
	        return new CommitInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stagedCount = source["stagedCount"];
	        this.canAmend = source["canAmend"];
	        this.lastMessage = source["lastMessage"];
	        this.pushed = source["pushed"];
	        this.upstream = source["upstream"];
	    }
	}
	export class FileStatus {
	    path: string;
	    oldPath?: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new FileStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.status = source["status"];
	    }
	}
	export class State {
	    staged: FileStatus[];
	    unstaged: FileStatus[];
	    untracked: FileStatus[];
	    merging: boolean;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.staged = this.convertValues(source["staged"], FileStatus);
	        this.unstaged = this.convertValues(source["unstaged"], FileStatus);
	        this.untracked = this.convertValues(source["untracked"], FileStatus);
	        this.merging = source["merging"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

