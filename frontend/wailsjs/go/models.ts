export namespace accounts {
	
	export class Jira {
	    site: string;
	    keys: string[];
	
	    static createFrom(source: any = {}) {
	        return new Jira(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.site = source["site"];
	        this.keys = source["keys"];
	    }
	}

}

export namespace ai {
	
	export class Usage {
	    input: number;
	    output: number;
	    cacheRead?: number;
	    cacheWrite?: number;
	
	    static createFrom(source: any = {}) {
	        return new Usage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input = source["input"];
	        this.output = source["output"];
	        this.cacheRead = source["cacheRead"];
	        this.cacheWrite = source["cacheWrite"];
	    }
	}
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
	    limited?: number;
	    resolve?: boolean;
	    provider?: string;
	    model?: string;
	    at?: string;
	    usage?: Usage;
	
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
	        this.limited = source["limited"];
	        this.resolve = source["resolve"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.at = source["at"];
	        this.usage = this.convertValues(source["usage"], Usage);
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
	export class AddReposResult {
	    added: repos.Repo[];
	    failed: repos.AddFailure[];
	
	    static createFrom(source: any = {}) {
	        return new AddReposResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = this.convertValues(source["added"], repos.Repo);
	        this.failed = this.convertValues(source["failed"], repos.AddFailure);
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
	export class CloneState {
	    running: boolean;
	    url: string;
	    dest: string;
	    progress?: clone.Progress;
	    lastError: string;
	
	    static createFrom(source: any = {}) {
	        return new CloneState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.url = source["url"];
	        this.dest = source["dest"];
	        this.progress = this.convertValues(source["progress"], clone.Progress);
	        this.lastError = source["lastError"];
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
	export class CommandLogView {
	    repo: string;
	    entries: cmdlog.Entry[];
	
	    static createFrom(source: any = {}) {
	        return new CommandLogView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo = source["repo"];
	        this.entries = this.convertValues(source["entries"], cmdlog.Entry);
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
	export class ConfirmEvent {
	    repoID: string;
	    runID: string;
	    confirmID: string;
	    tool: string;
	    title: string;
	    details: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConfirmEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repoID = source["repoID"];
	        this.runID = source["runID"];
	        this.confirmID = source["confirmID"];
	        this.tool = source["tool"];
	        this.title = source["title"];
	        this.details = source["details"];
	    }
	}
	export class Region {
	    id: string;
	    start: number;
	    end: number;
	    baseAt: number;
	    sep: number;
	
	    static createFrom(source: any = {}) {
	        return new Region(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.baseAt = source["baseAt"];
	        this.sep = source["sep"];
	    }
	}
	export class ConflictFile {
	    path: string;
	    resolved: boolean;
	    text: string;
	    regions: Region[];
	    restartable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConflictFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.resolved = source["resolved"];
	        this.text = source["text"];
	        this.regions = this.convertValues(source["regions"], Region);
	        this.restartable = source["restartable"];
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
	export class HostingAccount {
	    host: string;
	    connected: boolean;
	    rejected: boolean;
	    name: string;
	    email: string;
	    source: string;
	    ghAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HostingAccount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.connected = source["connected"];
	        this.rejected = source["rejected"];
	        this.name = source["name"];
	        this.email = source["email"];
	        this.source = source["source"];
	        this.ghAvailable = source["ghAvailable"];
	    }
	}
	export class HostingInfo {
	    supported: boolean;
	    host: string;
	    owner: string;
	    name: string;
	    remote: string;
	    webURL: string;
	    pullsURL: string;
	    noun: string;
	    account: string;
	
	    static createFrom(source: any = {}) {
	        return new HostingInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.host = source["host"];
	        this.owner = source["owner"];
	        this.name = source["name"];
	        this.remote = source["remote"];
	        this.webURL = source["webURL"];
	        this.pullsURL = source["pullsURL"];
	        this.noun = source["noun"];
	        this.account = source["account"];
	    }
	}
	export class Identity {
	    name: string;
	    email: string;
	
	    static createFrom(source: any = {}) {
	        return new Identity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.email = source["email"];
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
	
	export class Notification {
	    id: string;
	    title: string;
	    body: string;
	    repoID: string;
	    target: string;
	    hash?: string;
	
	    static createFrom(source: any = {}) {
	        return new Notification(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.body = source["body"];
	        this.repoID = source["repoID"];
	        this.target = source["target"];
	        this.hash = source["hash"];
	    }
	}
	
	
	export class PullRequestList {
	    items: hosting.PullRequest[];
	    more: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PullRequestList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], hosting.PullRequest);
	        this.more = source["more"];
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
	
	export class RegionResult {
	    left: number;
	    staged: boolean;
	    settled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RegionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.left = source["left"];
	        this.staged = source["staged"];
	        this.settled = source["settled"];
	    }
	}
	export class RepoAIInfo {
	    effective: settings.Settings;
	    global: settings.Settings;
	    overrides: reposettings.Override;
	    aiOff: boolean;
	    repoInstructions: reposettings.RepoInstructions;
	    state: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RepoAIInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.effective = this.convertValues(source["effective"], settings.Settings);
	        this.global = this.convertValues(source["global"], settings.Settings);
	        this.overrides = this.convertValues(source["overrides"], reposettings.Override);
	        this.aiOff = source["aiOff"];
	        this.repoInstructions = this.convertValues(source["repoInstructions"], reposettings.RepoInstructions);
	        this.state = source["state"];
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
	export class RepoItem {
	    id: string;
	    name: string;
	    path: string;
	    missing: boolean;
	    group?: string;
	    branch: string;
	    parentId?: string;
	    worktree?: boolean;
	    submodule?: boolean;
	    subPath?: string;
	    submoduleCount?: number;
	
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
	        this.parentId = source["parentId"];
	        this.worktree = source["worktree"];
	        this.submodule = source["submodule"];
	        this.subPath = source["subPath"];
	        this.submoduleCount = source["submoduleCount"];
	    }
	}
	export class ScanCandidate {
	    name: string;
	    path: string;
	    added: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ScanCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.added = source["added"];
	    }
	}
	export class ScanResult {
	    parent: string;
	    repos: ScanCandidate[];
	    skipped: number;
	
	    static createFrom(source: any = {}) {
	        return new ScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.parent = source["parent"];
	        this.repos = this.convertValues(source["repos"], ScanCandidate);
	        this.skipped = source["skipped"];
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
	export class UpdateInfo {
	    current: string;
	    latest: string;
	    newer: boolean;
	    pageURL: string;
	    canInstall: boolean;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.newer = source["newer"];
	        this.pageURL = source["pageURL"];
	        this.canInstall = source["canInstall"];
	        this.reason = source["reason"];
	    }
	}
	export class WorktreeDiff {
	    text: string;
	    hash: string;
	    truncated: boolean;
	    patchable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorktreeDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.hash = source["hash"];
	        this.truncated = source["truncated"];
	        this.patchable = source["patchable"];
	    }
	}

}

export namespace clone {
	
	export class Progress {
	    phase: string;
	    percent: number;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new Progress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.percent = source["percent"];
	        this.detail = source["detail"];
	    }
	}

}

export namespace cmdlog {
	
	export class Entry {
	    id: number;
	    repo: string;
	    repoId: string;
	    args: string[];
	    origin: string;
	    kind: string;
	    // Go type: time
	    start: any;
	    durationMs: number;
	    exitCode: number;
	    outcome: string;
	    outputTruncated: boolean;
	    outputDropped: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.repo = source["repo"];
	        this.repoId = source["repoId"];
	        this.args = source["args"];
	        this.origin = source["origin"];
	        this.kind = source["kind"];
	        this.start = this.convertValues(source["start"], null);
	        this.durationMs = source["durationMs"];
	        this.exitCode = source["exitCode"];
	        this.outcome = source["outcome"];
	        this.outputTruncated = source["outputTruncated"];
	        this.outputDropped = source["outputDropped"];
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
	export class Output {
	    stdout: string;
	    stderr: string;
	
	    static createFrom(source: any = {}) {
	        return new Output(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stdout = source["stdout"];
	        this.stderr = source["stderr"];
	    }
	}

}

export namespace credprompt {
	
	export class Answer {
	    username: string;
	    secret: string;
	    remember: boolean;
	    trust: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Answer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.username = source["username"];
	        this.secret = source["secret"];
	        this.remember = source["remember"];
	        this.trust = source["trust"];
	    }
	}

}

export namespace gitflow {
	
	export class Prefixes {
	    feature: string;
	    release: string;
	    hotfix: string;
	    warmfix: string;
	
	    static createFrom(source: any = {}) {
	        return new Prefixes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.feature = source["feature"];
	        this.release = source["release"];
	        this.hotfix = source["hotfix"];
	        this.warmfix = source["warmfix"];
	    }
	}
	export class Config {
	    master: string;
	    develop: string;
	    prefixes: Prefixes;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.master = source["master"];
	        this.develop = source["develop"];
	        this.prefixes = this.convertValues(source["prefixes"], Prefixes);
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
	export class FinishResult {
	    outcome: string;
	    target: string;
	    conflicts: string[];
	    merged: string[];
	    notes: string[];
	
	    static createFrom(source: any = {}) {
	        return new FinishResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.target = source["target"];
	        this.conflicts = source["conflicts"];
	        this.merged = source["merged"];
	        this.notes = source["notes"];
	    }
	}
	export class FlowBranch {
	    name: string;
	    type: string;
	    short: string;
	    base: string;
	    inProgress: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FlowBranch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.short = source["short"];
	        this.base = source["base"];
	        this.inProgress = source["inProgress"];
	    }
	}
	export class Flow {
	    initialized: boolean;
	    problem: string;
	    master: string;
	    develop: string;
	    prefixes: Prefixes;
	    current?: FlowBranch;
	    branches: FlowBranch[];
	    releases: string[];
	
	    static createFrom(source: any = {}) {
	        return new Flow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.initialized = source["initialized"];
	        this.problem = source["problem"];
	        this.master = source["master"];
	        this.develop = source["develop"];
	        this.prefixes = this.convertValues(source["prefixes"], Prefixes);
	        this.current = this.convertValues(source["current"], FlowBranch);
	        this.branches = this.convertValues(source["branches"], FlowBranch);
	        this.releases = source["releases"];
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
	
	export class Step {
	    target: string;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Step(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.done = source["done"];
	    }
	}
	export class Plan {
	    branch: string;
	    type: string;
	    steps: Step[];
	    ending: string;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.type = source["type"];
	        this.steps = this.convertValues(source["steps"], Step);
	        this.ending = source["ending"];
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
	
	export class Settings {
	    initialized: boolean;
	    problem: string;
	    master: string;
	    develop: string;
	    prefixes: Prefixes;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.initialized = source["initialized"];
	        this.problem = source["problem"];
	        this.master = source["master"];
	        this.develop = source["develop"];
	        this.prefixes = this.convertValues(source["prefixes"], Prefixes);
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
	export class StartResult {
	    branch: string;
	    notes: string[];
	
	    static createFrom(source: any = {}) {
	        return new StartResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.notes = source["notes"];
	    }
	}

}

export namespace gitlog {
	
	export class BlameBlock {
	    hash: string;
	    short: string;
	    author: string;
	    email: string;
	    // Go type: time
	    date: any;
	    summary: string;
	    filename: string;
	    start: number;
	    count: number;
	    previous?: string;
	    prevPath?: string;
	    boundary?: boolean;
	    uncommitted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BlameBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.short = source["short"];
	        this.author = source["author"];
	        this.email = source["email"];
	        this.date = this.convertValues(source["date"], null);
	        this.summary = source["summary"];
	        this.filename = source["filename"];
	        this.start = source["start"];
	        this.count = source["count"];
	        this.previous = source["previous"];
	        this.prevPath = source["prevPath"];
	        this.boundary = source["boundary"];
	        this.uncommitted = source["uncommitted"];
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
	export class Blame {
	    path: string;
	    rev: string;
	    startLine: number;
	    lines: string[];
	    blocks: BlameBlock[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Blame(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.rev = source["rev"];
	        this.startLine = source["startLine"];
	        this.lines = source["lines"];
	        this.blocks = this.convertValues(source["blocks"], BlameBlock);
	        this.truncated = source["truncated"];
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
	
	export class FileChange {
	    status: string;
	    path: string;
	    oldPath?: string;
	    submodule?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.submodule = source["submodule"];
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
	    pushScope: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pullStrategy = source["pullStrategy"];
	        this.pushScope = source["pushScope"];
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

export namespace hosting {
	
	export class Check {
	    key: string;
	    name: string;
	    state: string;
	    url: string;
	    // Go type: time
	    started: any;
	    durationSec: number;
	    // Go type: time
	    created: any;
	
	    static createFrom(source: any = {}) {
	        return new Check(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.state = source["state"];
	        this.url = source["url"];
	        this.started = this.convertValues(source["started"], null);
	        this.durationSec = source["durationSec"];
	        this.created = this.convertValues(source["created"], null);
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
	export class Comment {
	    id: string;
	    parent: string;
	    author: string;
	    // Go type: time
	    created: any;
	    body: string;
	    path: string;
	    line: number;
	    verdict: string;
	
	    static createFrom(source: any = {}) {
	        return new Comment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.parent = source["parent"];
	        this.author = source["author"];
	        this.created = this.convertValues(source["created"], null);
	        this.body = source["body"];
	        this.path = source["path"];
	        this.line = source["line"];
	        this.verdict = source["verdict"];
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
	export class CommitStatus {
	    hash: string;
	    state: string;
	    checks: Check[];
	
	    static createFrom(source: any = {}) {
	        return new CommitStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.state = source["state"];
	        this.checks = this.convertValues(source["checks"], Check);
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
	export class Count {
	    n: number;
	    more: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Count(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.n = source["n"];
	        this.more = source["more"];
	    }
	}
	export class PRCommit {
	    hash: string;
	    short: string;
	    subject: string;
	    author: string;
	
	    static createFrom(source: any = {}) {
	        return new PRCommit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.short = source["short"];
	        this.subject = source["subject"];
	        this.author = source["author"];
	    }
	}
	export class PRFile {
	    path: string;
	    oldPath: string;
	    status: string;
	    patch: string;
	    noDiff: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PRFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.status = source["status"];
	        this.patch = source["patch"];
	        this.noDiff = source["noDiff"];
	    }
	}
	export class PullRequest {
	    number: number;
	    title: string;
	    draft: boolean;
	    author: string;
	    authorID: string;
	    source: string;
	    target: string;
	    headHash: string;
	    url: string;
	    fork: boolean;
	    approvals: number;
	    commentCount: number;
	    // Go type: time
	    updated: any;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new PullRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.number = source["number"];
	        this.title = source["title"];
	        this.draft = source["draft"];
	        this.author = source["author"];
	        this.authorID = source["authorID"];
	        this.source = source["source"];
	        this.target = source["target"];
	        this.headHash = source["headHash"];
	        this.url = source["url"];
	        this.fork = source["fork"];
	        this.approvals = source["approvals"];
	        this.commentCount = source["commentCount"];
	        this.updated = this.convertValues(source["updated"], null);
	        this.state = source["state"];
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
	export class Reviewer {
	    name: string;
	    verdict: string;
	
	    static createFrom(source: any = {}) {
	        return new Reviewer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.verdict = source["verdict"];
	    }
	}
	export class PullRequestDetail {
	    number: number;
	    title: string;
	    draft: boolean;
	    author: string;
	    authorID: string;
	    source: string;
	    target: string;
	    headHash: string;
	    url: string;
	    fork: boolean;
	    approvals: number;
	    commentCount: number;
	    // Go type: time
	    updated: any;
	    state: string;
	    description: string;
	    // Go type: time
	    created: any;
	    reviewers: Reviewer[];
	    commits: PRCommit[];
	    comments: Comment[];
	    commentsMore: boolean;
	    checks: CommitStatus;
	
	    static createFrom(source: any = {}) {
	        return new PullRequestDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.number = source["number"];
	        this.title = source["title"];
	        this.draft = source["draft"];
	        this.author = source["author"];
	        this.authorID = source["authorID"];
	        this.source = source["source"];
	        this.target = source["target"];
	        this.headHash = source["headHash"];
	        this.url = source["url"];
	        this.fork = source["fork"];
	        this.approvals = source["approvals"];
	        this.commentCount = source["commentCount"];
	        this.updated = this.convertValues(source["updated"], null);
	        this.state = source["state"];
	        this.description = source["description"];
	        this.created = this.convertValues(source["created"], null);
	        this.reviewers = this.convertValues(source["reviewers"], Reviewer);
	        this.commits = this.convertValues(source["commits"], PRCommit);
	        this.comments = this.convertValues(source["comments"], Comment);
	        this.commentsMore = source["commentsMore"];
	        this.checks = this.convertValues(source["checks"], CommitStatus);
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

export namespace keys {
	
	export class Accelerator {
	    Key: string;
	    Modifiers: string[];
	
	    static createFrom(source: any = {}) {
	        return new Accelerator(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Key = source["Key"];
	        this.Modifiers = source["Modifiers"];
	    }
	}

}

export namespace menu {
	
	export class MenuItem {
	    Label: string;
	    Role: number;
	    Accelerator?: keys.Accelerator;
	    Type: string;
	    Disabled: boolean;
	    Hidden: boolean;
	    Checked: boolean;
	    SubMenu?: Menu;
	
	    static createFrom(source: any = {}) {
	        return new MenuItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Label = source["Label"];
	        this.Role = source["Role"];
	        this.Accelerator = this.convertValues(source["Accelerator"], keys.Accelerator);
	        this.Type = source["Type"];
	        this.Disabled = source["Disabled"];
	        this.Hidden = source["Hidden"];
	        this.Checked = source["Checked"];
	        this.SubMenu = this.convertValues(source["SubMenu"], Menu);
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
	export class Menu {
	    Items: MenuItem[];
	
	    static createFrom(source: any = {}) {
	        return new Menu(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Items = this.convertValues(source["Items"], MenuItem);
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

export namespace merge {
	
	export class Preview {
	    commits: number;
	    merges: number;
	    published: number;
	    upstream: string;
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.commits = source["commits"];
	        this.merges = source["merges"];
	        this.published = source["published"];
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
	    oursLabel?: string;
	    theirsLabel?: string;
	    oursDescription?: string;
	    theirsDescription?: string;
	
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
	        this.oursLabel = source["oursLabel"];
	        this.theirsLabel = source["theirsLabel"];
	        this.oursDescription = source["oursDescription"];
	        this.theirsDescription = source["theirsDescription"];
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
	export class AutoFetchResult {
	    skipped: boolean;
	    branch: string;
	    upstream: string;
	    newCommits: number;
	    refsChanged: boolean;
	    authFailed: string[];
	
	    static createFrom(source: any = {}) {
	        return new AutoFetchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skipped = source["skipped"];
	        this.branch = source["branch"];
	        this.upstream = source["upstream"];
	        this.newCommits = source["newCommits"];
	        this.refsChanged = source["refsChanged"];
	        this.authFailed = source["authFailed"];
	    }
	}
	export class BranchPushResult {
	    branch: string;
	    target: string;
	    status: string;
	    reason?: string;
	    tags?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BranchPushResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.target = source["target"];
	        this.status = source["status"];
	        this.reason = source["reason"];
	        this.tags = source["tags"];
	    }
	}
	export class PushSpec {
	    local: string;
	    remote: string;
	    track: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PushSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.local = source["local"];
	        this.remote = source["remote"];
	        this.track = source["track"];
	    }
	}
	export class PushRequest {
	    remote: string;
	    branches: PushSpec[];
	    tags: boolean;
	    force: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PushRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.remote = source["remote"];
	        this.branches = this.convertValues(source["branches"], PushSpec);
	        this.tags = source["tags"];
	        this.force = source["force"];
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
	
	export class Remote {
	    name: string;
	    fetchURL: string;
	    pushURL: string;
	
	    static createFrom(source: any = {}) {
	        return new Remote(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.fetchURL = source["fetchURL"];
	        this.pushURL = source["pushURL"];
	    }
	}
	export class RemoteTest {
	    ok: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
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
	    tracked: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.conflicts = source["conflicts"];
	        this.tracked = source["tracked"];
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
	    ahead?: number;
	    behind?: number;
	    upstreamGone?: boolean;
	    upstreamLocal?: boolean;
	    official: boolean;
	    worktree?: string;
	    worktreeGone?: boolean;
	
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
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.upstreamGone = source["upstreamGone"];
	        this.upstreamLocal = source["upstreamLocal"];
	        this.official = source["official"];
	        this.worktree = source["worktree"];
	        this.worktreeGone = source["worktreeGone"];
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
	
	
	export class Tracking {
	    twin: string;
	    tracked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Tracking(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.twin = source["twin"];
	        this.tracked = source["tracked"];
	    }
	}

}

export namespace repoinfo {
	
	export class Info {
	    loose: number;
	    looseSize: number;
	    packed: number;
	    packs: number;
	    packSize: number;
	    prunePackable: number;
	    garbage: number;
	    garbageSize: number;
	    size: number;
	    autoLimit: number;
	    packLimit: number;
	    gcReason: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loose = source["loose"];
	        this.looseSize = source["looseSize"];
	        this.packed = source["packed"];
	        this.packs = source["packs"];
	        this.packSize = source["packSize"];
	        this.prunePackable = source["prunePackable"];
	        this.garbage = source["garbage"];
	        this.garbageSize = source["garbageSize"];
	        this.size = source["size"];
	        this.autoLimit = source["autoLimit"];
	        this.packLimit = source["packLimit"];
	        this.gcReason = source["gcReason"];
	    }
	}

}

export namespace repos {
	
	export class AddFailure {
	    path: string;
	    name: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new AddFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.reason = source["reason"];
	    }
	}
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

export namespace reposettings {
	
	export class Override {
	    aiOff?: boolean;
	    chatProvider?: string;
	    chatModel?: string;
	    taskProvider?: string;
	    taskModel?: string;
	    commitMessage?: string;
	    suggestReplies?: string;
	    instructions?: Record<string, string>;
	    approvedRepoInstructions?: string;
	
	    static createFrom(source: any = {}) {
	        return new Override(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.aiOff = source["aiOff"];
	        this.chatProvider = source["chatProvider"];
	        this.chatModel = source["chatModel"];
	        this.taskProvider = source["taskProvider"];
	        this.taskModel = source["taskModel"];
	        this.commitMessage = source["commitMessage"];
	        this.suggestReplies = source["suggestReplies"];
	        this.instructions = source["instructions"];
	        this.approvedRepoInstructions = source["approvedRepoInstructions"];
	    }
	}
	export class RepoFile {
	    name: string;
	    text: string;
	    ignored?: string;
	
	    static createFrom(source: any = {}) {
	        return new RepoFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.text = source["text"];
	        this.ignored = source["ignored"];
	    }
	}
	export class RepoInstructions {
	    files: RepoFile[];
	    hash: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RepoInstructions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = this.convertValues(source["files"], RepoFile);
	        this.hash = source["hash"];
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

}

export namespace settings {
	
	export class Settings {
	    ollamaURL: string;
	    chatProvider: string;
	    chatModel: string;
	    taskProvider: string;
	    taskModel: string;
	    commitMessage: string;
	    suggestReplies: string;
	    chatToolLimit: number;
	    resolveToolLimit: number;
	
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
	        this.suggestReplies = source["suggestReplies"];
	        this.chatToolLimit = source["chatToolLimit"];
	        this.resolveToolLimit = source["resolveToolLimit"];
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

export namespace submodules {
	
	export class Submodule {
	    name: string;
	    path: string;
	    url: string;
	    recorded: string;
	    checkedOut: string;
	    branch: string;
	    initialised: boolean;
	    configured: boolean;
	    moved: boolean;
	    modified: boolean;
	    untracked: boolean;
	    conflict: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Submodule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.url = source["url"];
	        this.recorded = source["recorded"];
	        this.checkedOut = source["checkedOut"];
	        this.branch = source["branch"];
	        this.initialised = source["initialised"];
	        this.configured = source["configured"];
	        this.moved = source["moved"];
	        this.modified = source["modified"];
	        this.untracked = source["untracked"];
	        this.conflict = source["conflict"];
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
	    submodule?: boolean;
	    subCommit?: boolean;
	    subModified?: boolean;
	    subUntracked?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.status = source["status"];
	        this.submodule = source["submodule"];
	        this.subCommit = source["subCommit"];
	        this.subModified = source["subModified"];
	        this.subUntracked = source["subUntracked"];
	    }
	}
	export class HunkPick {
	    hunk: number;
	    lines: number[];
	
	    static createFrom(source: any = {}) {
	        return new HunkPick(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hunk = source["hunk"];
	        this.lines = source["lines"];
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

export namespace worktrees {
	
	export class RemovalInfo {
	    branch: string;
	    detached: boolean;
	    changes: number;
	    locked: boolean;
	    merged: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemovalInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.detached = source["detached"];
	        this.changes = source["changes"];
	        this.locked = source["locked"];
	        this.merged = source["merged"];
	    }
	}

}

