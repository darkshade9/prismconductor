export namespace types {
	
	export class ConventionHints {
	    test_command: string;
	    build_command: string;
	    lint_command: string;
	    py_env_path: string;
	    package_manager: string;
	
	    static createFrom(source: any = {}) {
	        return new ConventionHints(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.test_command = source["test_command"];
	        this.build_command = source["build_command"];
	        this.lint_command = source["lint_command"];
	        this.py_env_path = source["py_env_path"];
	        this.package_manager = source["package_manager"];
	    }
	}
	export class EnvSpec {
	    env_vars: Record<string, string>;
	    pre_commands: string[];
	    shell: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.env_vars = source["env_vars"];
	        this.pre_commands = source["pre_commands"];
	        this.shell = source["shell"];
	    }
	}
	export class Session {
	    id: string;
	    workspace_id: string;
	    issue_number: number;
	    mode: string;
	    state: string;
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    ended_at?: any;
	    pid: number;
	    last_prompt: string;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspace_id = source["workspace_id"];
	        this.issue_number = source["issue_number"];
	        this.mode = source["mode"];
	        this.state = source["state"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.ended_at = this.convertValues(source["ended_at"], null);
	        this.pid = source["pid"];
	        this.last_prompt = source["last_prompt"];
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
	export class SkillProfile {
	    mode: string;
	    use_conductor_plan: boolean;
	    use_conductor_execute: boolean;
	    use_conductor_close: boolean;
	    native_plan_command: string;
	    native_execute_command: string;
	    native_close_command: string;
	    extra_context_files: string[];
	
	    static createFrom(source: any = {}) {
	        return new SkillProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.use_conductor_plan = source["use_conductor_plan"];
	        this.use_conductor_execute = source["use_conductor_execute"];
	        this.use_conductor_close = source["use_conductor_close"];
	        this.native_plan_command = source["native_plan_command"];
	        this.native_execute_command = source["native_execute_command"];
	        this.native_close_command = source["native_close_command"];
	        this.extra_context_files = source["extra_context_files"];
	    }
	}
	export class Workspace {
	    id: string;
	    display_name: string;
	    repo_path: string;
	    github_owner: string;
	    github_repo: string;
	    default_branch: string;
	    color: string;
	    agent_env: EnvSpec;
	    skill_profile: SkillProfile;
	    conventions: ConventionHints;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.display_name = source["display_name"];
	        this.repo_path = source["repo_path"];
	        this.github_owner = source["github_owner"];
	        this.github_repo = source["github_repo"];
	        this.default_branch = source["default_branch"];
	        this.color = source["color"];
	        this.agent_env = this.convertValues(source["agent_env"], EnvSpec);
	        this.skill_profile = this.convertValues(source["skill_profile"], SkillProfile);
	        this.conventions = this.convertValues(source["conventions"], ConventionHints);
	        this.enabled = source["enabled"];
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

export namespace workspace {
	
	export class OnboardCheck {
	    Name: string;
	    Pass: boolean;
	    Info: string;
	
	    static createFrom(source: any = {}) {
	        return new OnboardCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Pass = source["Pass"];
	        this.Info = source["Info"];
	    }
	}

}

