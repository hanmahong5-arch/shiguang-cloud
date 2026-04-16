export namespace control {
	
	export class ServerLine {
	    id: string;
	    name: string;
	    auth_port: number;
	    game_args: string;
	    client_path: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.auth_port = source["auth_port"];
	        this.game_args = source["game_args"];
	        this.client_path = source["client_path"];
	    }
	}
	export class LauncherConfig {
	    public_gate_ip: string;
	    patch_manifest_url: string;
	    news_url: string;
	    servers: ServerLine[];
	
	    static createFrom(source: any = {}) {
	        return new LauncherConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.public_gate_ip = source["public_gate_ip"];
	        this.patch_manifest_url = source["patch_manifest_url"];
	        this.news_url = source["news_url"];
	        this.servers = this.convertValues(source["servers"], ServerLine);
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

export namespace main {
	
	export class BrandServer {
	    id: string;
	    name: string;
	    auth_port: number;
	    game_args: string;
	
	    static createFrom(source: any = {}) {
	        return new BrandServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.auth_port = source["auth_port"];
	        this.game_args = source["game_args"];
	    }
	}
	export class BrandConfig {
	    server_code: string;
	    server_name: string;
	    logo_url: string;
	    bg_url: string;
	    accent_color: string;
	    text_color: string;
	    control_url: string;
	    gate_ip: string;
	    news_url: string;
	    servers: BrandServer[];
	
	    static createFrom(source: any = {}) {
	        return new BrandConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server_code = source["server_code"];
	        this.server_name = source["server_name"];
	        this.logo_url = source["logo_url"];
	        this.bg_url = source["bg_url"];
	        this.accent_color = source["accent_color"];
	        this.text_color = source["text_color"];
	        this.control_url = source["control_url"];
	        this.gate_ip = source["gate_ip"];
	        this.news_url = source["news_url"];
	        this.servers = this.convertValues(source["servers"], BrandServer);
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
	
	export class Prefs {
	    control_url: string;
	    client_paths: Record<string, string>;
	    server_code: string;
	
	    static createFrom(source: any = {}) {
	        return new Prefs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.control_url = source["control_url"];
	        this.client_paths = source["client_paths"];
	        this.server_code = source["server_code"];
	    }
	}

}

