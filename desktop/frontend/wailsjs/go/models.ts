export namespace main {
	
	export class Status {
	    node_id: string;
	    name: string;
	    listen: string;
	    advertise: string;
	    openai: string;
	    ws: string;
	    relays: string[];
	    password?: string;
	    engines: string[];
	    linked: boolean;
	    link_error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.node_id = source["node_id"];
	        this.name = source["name"];
	        this.listen = source["listen"];
	        this.advertise = source["advertise"];
	        this.openai = source["openai"];
	        this.ws = source["ws"];
	        this.relays = source["relays"];
	        this.password = source["password"];
	        this.engines = source["engines"];
	        this.linked = source["linked"];
	        this.link_error = source["link_error"];
	    }
	}

}

export namespace protocol {
	
	export class EngineInfo {
	    name: string;
	    title?: string;
	    enabled: boolean;
	    available: boolean;
	    binary?: string;
	    version?: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new EngineInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.title = source["title"];
	        this.enabled = source["enabled"];
	        this.available = source["available"];
	        this.binary = source["binary"];
	        this.version = source["version"];
	        this.detail = source["detail"];
	    }
	}
	export class Event {
	    type: string;
	    role?: string;
	    tool?: string;
	    engine?: string;
	    session?: string;
	    text?: string;
	    preview?: string;
	    unix?: number;
	    done?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.role = source["role"];
	        this.tool = source["tool"];
	        this.engine = source["engine"];
	        this.session = source["session"];
	        this.text = source["text"];
	        this.preview = source["preview"];
	        this.unix = source["unix"];
	        this.done = source["done"];
	    }
	}
	export class Model {
	    id: string;
	    engine: string;
	    name: string;
	    title?: string;
	    default?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Model(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.engine = source["engine"];
	        this.name = source["name"];
	        this.title = source["title"];
	        this.default = source["default"];
	    }
	}
	export class Session {
	    engine: string;
	    id: string;
	    title?: string;
	    cwd?: string;
	    status?: string;
	    kind?: string;
	    pid?: number;
	    live: boolean;
	    busy: boolean;
	    updated_at?: number;
	    jsonl?: string;
	    extra?: string;
	    node_id?: string;
	    node_name?: string;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine = source["engine"];
	        this.id = source["id"];
	        this.title = source["title"];
	        this.cwd = source["cwd"];
	        this.status = source["status"];
	        this.kind = source["kind"];
	        this.pid = source["pid"];
	        this.live = source["live"];
	        this.busy = source["busy"];
	        this.updated_at = source["updated_at"];
	        this.jsonl = source["jsonl"];
	        this.extra = source["extra"];
	        this.node_id = source["node_id"];
	        this.node_name = source["node_name"];
	    }
	}
	export class WorkDir {
	    path: string;
	    engine?: string;
	    count?: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkDir(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.engine = source["engine"];
	        this.count = source["count"];
	    }
	}

}

