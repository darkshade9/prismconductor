export namespace diagnose {
	
	export class IssueDiagnosis {
	    summary: string;
	    detail: string[];
	    suggestion: string;
	    severity_level: string;
	
	    static createFrom(source: any = {}) {
	        return new IssueDiagnosis(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = source["summary"];
	        this.detail = source["detail"];
	        this.suggestion = source["suggestion"];
	        this.severity_level = source["severity_level"];
	    }
	}

}

export namespace github {
	
	export class AdvancedSecurity {
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new AdvancedSecurity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	    }
	}
	export class CodeOfConduct {
	    name?: string;
	    key?: string;
	    url?: string;
	    body?: string;
	
	    static createFrom(source: any = {}) {
	        return new CodeOfConduct(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.key = source["key"];
	        this.url = source["url"];
	        this.body = source["body"];
	    }
	}
	export class DependabotSecurityUpdates {
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new DependabotSecurityUpdates(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	    }
	}
	export class License {
	    key?: string;
	    name?: string;
	    url?: string;
	    spdx_id?: string;
	    html_url?: string;
	    featured?: boolean;
	    description?: string;
	    implementation?: string;
	    permissions?: string[];
	    conditions?: string[];
	    limitations?: string[];
	    body?: string;
	
	    static createFrom(source: any = {}) {
	        return new License(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.url = source["url"];
	        this.spdx_id = source["spdx_id"];
	        this.html_url = source["html_url"];
	        this.featured = source["featured"];
	        this.description = source["description"];
	        this.implementation = source["implementation"];
	        this.permissions = source["permissions"];
	        this.conditions = source["conditions"];
	        this.limitations = source["limitations"];
	        this.body = source["body"];
	    }
	}
	export class Match {
	    text?: string;
	    indices?: number[];
	
	    static createFrom(source: any = {}) {
	        return new Match(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.indices = source["indices"];
	    }
	}
	export class Plan {
	    name?: string;
	    space?: number;
	    collaborators?: number;
	    private_repos?: number;
	    filled_seats?: number;
	    seats?: number;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.space = source["space"];
	        this.collaborators = source["collaborators"];
	        this.private_repos = source["private_repos"];
	        this.filled_seats = source["filled_seats"];
	        this.seats = source["seats"];
	    }
	}
	export class Timestamp {
	
	
	    static createFrom(source: any = {}) {
	        return new Timestamp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class Organization {
	    login?: string;
	    id?: number;
	    node_id?: string;
	    avatar_url?: string;
	    html_url?: string;
	    name?: string;
	    company?: string;
	    blog?: string;
	    location?: string;
	    email?: string;
	    twitter_username?: string;
	    description?: string;
	    public_repos?: number;
	    public_gists?: number;
	    followers?: number;
	    following?: number;
	    created_at?: Timestamp;
	    updated_at?: Timestamp;
	    total_private_repos?: number;
	    owned_private_repos?: number;
	    private_gists?: number;
	    disk_usage?: number;
	    collaborators?: number;
	    billing_email?: string;
	    type?: string;
	    plan?: Plan;
	    two_factor_requirement_enabled?: boolean;
	    is_verified?: boolean;
	    has_organization_projects?: boolean;
	    has_repository_projects?: boolean;
	    default_repository_permission?: string;
	    default_repository_settings?: string;
	    members_can_create_repositories?: boolean;
	    members_can_create_public_repositories?: boolean;
	    members_can_create_private_repositories?: boolean;
	    members_can_create_internal_repositories?: boolean;
	    members_can_fork_private_repositories?: boolean;
	    members_allowed_repository_creation_type?: string;
	    members_can_create_pages?: boolean;
	    members_can_create_public_pages?: boolean;
	    members_can_create_private_pages?: boolean;
	    web_commit_signoff_required?: boolean;
	    advanced_security_enabled_for_new_repositories?: boolean;
	    dependabot_alerts_enabled_for_new_repositories?: boolean;
	    dependabot_security_updates_enabled_for_new_repositories?: boolean;
	    dependency_graph_enabled_for_new_repositories?: boolean;
	    secret_scanning_enabled_for_new_repositories?: boolean;
	    secret_scanning_push_protection_enabled_for_new_repositories?: boolean;
	    secret_scanning_validity_checks_enabled?: boolean;
	    url?: string;
	    events_url?: string;
	    hooks_url?: string;
	    issues_url?: string;
	    members_url?: string;
	    public_members_url?: string;
	    repos_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new Organization(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.login = source["login"];
	        this.id = source["id"];
	        this.node_id = source["node_id"];
	        this.avatar_url = source["avatar_url"];
	        this.html_url = source["html_url"];
	        this.name = source["name"];
	        this.company = source["company"];
	        this.blog = source["blog"];
	        this.location = source["location"];
	        this.email = source["email"];
	        this.twitter_username = source["twitter_username"];
	        this.description = source["description"];
	        this.public_repos = source["public_repos"];
	        this.public_gists = source["public_gists"];
	        this.followers = source["followers"];
	        this.following = source["following"];
	        this.created_at = this.convertValues(source["created_at"], Timestamp);
	        this.updated_at = this.convertValues(source["updated_at"], Timestamp);
	        this.total_private_repos = source["total_private_repos"];
	        this.owned_private_repos = source["owned_private_repos"];
	        this.private_gists = source["private_gists"];
	        this.disk_usage = source["disk_usage"];
	        this.collaborators = source["collaborators"];
	        this.billing_email = source["billing_email"];
	        this.type = source["type"];
	        this.plan = this.convertValues(source["plan"], Plan);
	        this.two_factor_requirement_enabled = source["two_factor_requirement_enabled"];
	        this.is_verified = source["is_verified"];
	        this.has_organization_projects = source["has_organization_projects"];
	        this.has_repository_projects = source["has_repository_projects"];
	        this.default_repository_permission = source["default_repository_permission"];
	        this.default_repository_settings = source["default_repository_settings"];
	        this.members_can_create_repositories = source["members_can_create_repositories"];
	        this.members_can_create_public_repositories = source["members_can_create_public_repositories"];
	        this.members_can_create_private_repositories = source["members_can_create_private_repositories"];
	        this.members_can_create_internal_repositories = source["members_can_create_internal_repositories"];
	        this.members_can_fork_private_repositories = source["members_can_fork_private_repositories"];
	        this.members_allowed_repository_creation_type = source["members_allowed_repository_creation_type"];
	        this.members_can_create_pages = source["members_can_create_pages"];
	        this.members_can_create_public_pages = source["members_can_create_public_pages"];
	        this.members_can_create_private_pages = source["members_can_create_private_pages"];
	        this.web_commit_signoff_required = source["web_commit_signoff_required"];
	        this.advanced_security_enabled_for_new_repositories = source["advanced_security_enabled_for_new_repositories"];
	        this.dependabot_alerts_enabled_for_new_repositories = source["dependabot_alerts_enabled_for_new_repositories"];
	        this.dependabot_security_updates_enabled_for_new_repositories = source["dependabot_security_updates_enabled_for_new_repositories"];
	        this.dependency_graph_enabled_for_new_repositories = source["dependency_graph_enabled_for_new_repositories"];
	        this.secret_scanning_enabled_for_new_repositories = source["secret_scanning_enabled_for_new_repositories"];
	        this.secret_scanning_push_protection_enabled_for_new_repositories = source["secret_scanning_push_protection_enabled_for_new_repositories"];
	        this.secret_scanning_validity_checks_enabled = source["secret_scanning_validity_checks_enabled"];
	        this.url = source["url"];
	        this.events_url = source["events_url"];
	        this.hooks_url = source["hooks_url"];
	        this.issues_url = source["issues_url"];
	        this.members_url = source["members_url"];
	        this.public_members_url = source["public_members_url"];
	        this.repos_url = source["repos_url"];
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
	
	export class SecretScanningValidityChecks {
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new SecretScanningValidityChecks(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	    }
	}
	export class SecretScanningPushProtection {
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new SecretScanningPushProtection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	    }
	}
	export class SecretScanning {
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new SecretScanning(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	    }
	}
	export class SecurityAndAnalysis {
	    advanced_security?: AdvancedSecurity;
	    secret_scanning?: SecretScanning;
	    secret_scanning_push_protection?: SecretScanningPushProtection;
	    dependabot_security_updates?: DependabotSecurityUpdates;
	    secret_scanning_validity_checks?: SecretScanningValidityChecks;
	
	    static createFrom(source: any = {}) {
	        return new SecurityAndAnalysis(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.advanced_security = this.convertValues(source["advanced_security"], AdvancedSecurity);
	        this.secret_scanning = this.convertValues(source["secret_scanning"], SecretScanning);
	        this.secret_scanning_push_protection = this.convertValues(source["secret_scanning_push_protection"], SecretScanningPushProtection);
	        this.dependabot_security_updates = this.convertValues(source["dependabot_security_updates"], DependabotSecurityUpdates);
	        this.secret_scanning_validity_checks = this.convertValues(source["secret_scanning_validity_checks"], SecretScanningValidityChecks);
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
	export class TextMatch {
	    object_url?: string;
	    object_type?: string;
	    property?: string;
	    fragment?: string;
	    matches?: Match[];
	
	    static createFrom(source: any = {}) {
	        return new TextMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.object_url = source["object_url"];
	        this.object_type = source["object_type"];
	        this.property = source["property"];
	        this.fragment = source["fragment"];
	        this.matches = this.convertValues(source["matches"], Match);
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
	export class User {
	    login?: string;
	    id?: number;
	    node_id?: string;
	    avatar_url?: string;
	    html_url?: string;
	    gravatar_id?: string;
	    name?: string;
	    company?: string;
	    blog?: string;
	    location?: string;
	    email?: string;
	    hireable?: boolean;
	    bio?: string;
	    twitter_username?: string;
	    public_repos?: number;
	    public_gists?: number;
	    followers?: number;
	    following?: number;
	    created_at?: Timestamp;
	    updated_at?: Timestamp;
	    suspended_at?: Timestamp;
	    type?: string;
	    site_admin?: boolean;
	    total_private_repos?: number;
	    owned_private_repos?: number;
	    private_gists?: number;
	    disk_usage?: number;
	    collaborators?: number;
	    two_factor_authentication?: boolean;
	    plan?: Plan;
	    ldap_dn?: string;
	    url?: string;
	    events_url?: string;
	    following_url?: string;
	    followers_url?: string;
	    gists_url?: string;
	    organizations_url?: string;
	    received_events_url?: string;
	    repos_url?: string;
	    starred_url?: string;
	    subscriptions_url?: string;
	    text_matches?: TextMatch[];
	    permissions?: Record<string, boolean>;
	    role_name?: string;
	
	    static createFrom(source: any = {}) {
	        return new User(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.login = source["login"];
	        this.id = source["id"];
	        this.node_id = source["node_id"];
	        this.avatar_url = source["avatar_url"];
	        this.html_url = source["html_url"];
	        this.gravatar_id = source["gravatar_id"];
	        this.name = source["name"];
	        this.company = source["company"];
	        this.blog = source["blog"];
	        this.location = source["location"];
	        this.email = source["email"];
	        this.hireable = source["hireable"];
	        this.bio = source["bio"];
	        this.twitter_username = source["twitter_username"];
	        this.public_repos = source["public_repos"];
	        this.public_gists = source["public_gists"];
	        this.followers = source["followers"];
	        this.following = source["following"];
	        this.created_at = this.convertValues(source["created_at"], Timestamp);
	        this.updated_at = this.convertValues(source["updated_at"], Timestamp);
	        this.suspended_at = this.convertValues(source["suspended_at"], Timestamp);
	        this.type = source["type"];
	        this.site_admin = source["site_admin"];
	        this.total_private_repos = source["total_private_repos"];
	        this.owned_private_repos = source["owned_private_repos"];
	        this.private_gists = source["private_gists"];
	        this.disk_usage = source["disk_usage"];
	        this.collaborators = source["collaborators"];
	        this.two_factor_authentication = source["two_factor_authentication"];
	        this.plan = this.convertValues(source["plan"], Plan);
	        this.ldap_dn = source["ldap_dn"];
	        this.url = source["url"];
	        this.events_url = source["events_url"];
	        this.following_url = source["following_url"];
	        this.followers_url = source["followers_url"];
	        this.gists_url = source["gists_url"];
	        this.organizations_url = source["organizations_url"];
	        this.received_events_url = source["received_events_url"];
	        this.repos_url = source["repos_url"];
	        this.starred_url = source["starred_url"];
	        this.subscriptions_url = source["subscriptions_url"];
	        this.text_matches = this.convertValues(source["text_matches"], TextMatch);
	        this.permissions = source["permissions"];
	        this.role_name = source["role_name"];
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
	export class Repository {
	    id?: number;
	    node_id?: string;
	    owner?: User;
	    name?: string;
	    full_name?: string;
	    description?: string;
	    homepage?: string;
	    code_of_conduct?: CodeOfConduct;
	    default_branch?: string;
	    master_branch?: string;
	    created_at?: Timestamp;
	    pushed_at?: Timestamp;
	    updated_at?: Timestamp;
	    html_url?: string;
	    clone_url?: string;
	    git_url?: string;
	    mirror_url?: string;
	    ssh_url?: string;
	    svn_url?: string;
	    language?: string;
	    fork?: boolean;
	    forks_count?: number;
	    network_count?: number;
	    open_issues_count?: number;
	    open_issues?: number;
	    stargazers_count?: number;
	    subscribers_count?: number;
	    watchers_count?: number;
	    watchers?: number;
	    size?: number;
	    auto_init?: boolean;
	    parent?: Repository;
	    source?: Repository;
	    template_repository?: Repository;
	    organization?: Organization;
	    permissions?: Record<string, boolean>;
	    allow_rebase_merge?: boolean;
	    allow_update_branch?: boolean;
	    allow_squash_merge?: boolean;
	    allow_merge_commit?: boolean;
	    allow_auto_merge?: boolean;
	    allow_forking?: boolean;
	    web_commit_signoff_required?: boolean;
	    delete_branch_on_merge?: boolean;
	    use_squash_pr_title_as_default?: boolean;
	    squash_merge_commit_title?: string;
	    squash_merge_commit_message?: string;
	    merge_commit_title?: string;
	    merge_commit_message?: string;
	    topics?: string[];
	    custom_properties?: Record<string, string>;
	    archived?: boolean;
	    disabled?: boolean;
	    license?: License;
	    private?: boolean;
	    has_issues?: boolean;
	    has_wiki?: boolean;
	    has_pages?: boolean;
	    has_projects?: boolean;
	    has_downloads?: boolean;
	    has_discussions?: boolean;
	    is_template?: boolean;
	    license_template?: string;
	    gitignore_template?: string;
	    security_and_analysis?: SecurityAndAnalysis;
	    team_id?: number;
	    url?: string;
	    archive_url?: string;
	    assignees_url?: string;
	    blobs_url?: string;
	    branches_url?: string;
	    collaborators_url?: string;
	    comments_url?: string;
	    commits_url?: string;
	    compare_url?: string;
	    contents_url?: string;
	    contributors_url?: string;
	    deployments_url?: string;
	    downloads_url?: string;
	    events_url?: string;
	    forks_url?: string;
	    git_commits_url?: string;
	    git_refs_url?: string;
	    git_tags_url?: string;
	    hooks_url?: string;
	    issue_comment_url?: string;
	    issue_events_url?: string;
	    issues_url?: string;
	    keys_url?: string;
	    labels_url?: string;
	    languages_url?: string;
	    merges_url?: string;
	    milestones_url?: string;
	    notifications_url?: string;
	    pulls_url?: string;
	    releases_url?: string;
	    stargazers_url?: string;
	    statuses_url?: string;
	    subscribers_url?: string;
	    subscription_url?: string;
	    tags_url?: string;
	    trees_url?: string;
	    teams_url?: string;
	    text_matches?: TextMatch[];
	    visibility?: string;
	    role_name?: string;
	
	    static createFrom(source: any = {}) {
	        return new Repository(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.node_id = source["node_id"];
	        this.owner = this.convertValues(source["owner"], User);
	        this.name = source["name"];
	        this.full_name = source["full_name"];
	        this.description = source["description"];
	        this.homepage = source["homepage"];
	        this.code_of_conduct = this.convertValues(source["code_of_conduct"], CodeOfConduct);
	        this.default_branch = source["default_branch"];
	        this.master_branch = source["master_branch"];
	        this.created_at = this.convertValues(source["created_at"], Timestamp);
	        this.pushed_at = this.convertValues(source["pushed_at"], Timestamp);
	        this.updated_at = this.convertValues(source["updated_at"], Timestamp);
	        this.html_url = source["html_url"];
	        this.clone_url = source["clone_url"];
	        this.git_url = source["git_url"];
	        this.mirror_url = source["mirror_url"];
	        this.ssh_url = source["ssh_url"];
	        this.svn_url = source["svn_url"];
	        this.language = source["language"];
	        this.fork = source["fork"];
	        this.forks_count = source["forks_count"];
	        this.network_count = source["network_count"];
	        this.open_issues_count = source["open_issues_count"];
	        this.open_issues = source["open_issues"];
	        this.stargazers_count = source["stargazers_count"];
	        this.subscribers_count = source["subscribers_count"];
	        this.watchers_count = source["watchers_count"];
	        this.watchers = source["watchers"];
	        this.size = source["size"];
	        this.auto_init = source["auto_init"];
	        this.parent = this.convertValues(source["parent"], Repository);
	        this.source = this.convertValues(source["source"], Repository);
	        this.template_repository = this.convertValues(source["template_repository"], Repository);
	        this.organization = this.convertValues(source["organization"], Organization);
	        this.permissions = source["permissions"];
	        this.allow_rebase_merge = source["allow_rebase_merge"];
	        this.allow_update_branch = source["allow_update_branch"];
	        this.allow_squash_merge = source["allow_squash_merge"];
	        this.allow_merge_commit = source["allow_merge_commit"];
	        this.allow_auto_merge = source["allow_auto_merge"];
	        this.allow_forking = source["allow_forking"];
	        this.web_commit_signoff_required = source["web_commit_signoff_required"];
	        this.delete_branch_on_merge = source["delete_branch_on_merge"];
	        this.use_squash_pr_title_as_default = source["use_squash_pr_title_as_default"];
	        this.squash_merge_commit_title = source["squash_merge_commit_title"];
	        this.squash_merge_commit_message = source["squash_merge_commit_message"];
	        this.merge_commit_title = source["merge_commit_title"];
	        this.merge_commit_message = source["merge_commit_message"];
	        this.topics = source["topics"];
	        this.custom_properties = source["custom_properties"];
	        this.archived = source["archived"];
	        this.disabled = source["disabled"];
	        this.license = this.convertValues(source["license"], License);
	        this.private = source["private"];
	        this.has_issues = source["has_issues"];
	        this.has_wiki = source["has_wiki"];
	        this.has_pages = source["has_pages"];
	        this.has_projects = source["has_projects"];
	        this.has_downloads = source["has_downloads"];
	        this.has_discussions = source["has_discussions"];
	        this.is_template = source["is_template"];
	        this.license_template = source["license_template"];
	        this.gitignore_template = source["gitignore_template"];
	        this.security_and_analysis = this.convertValues(source["security_and_analysis"], SecurityAndAnalysis);
	        this.team_id = source["team_id"];
	        this.url = source["url"];
	        this.archive_url = source["archive_url"];
	        this.assignees_url = source["assignees_url"];
	        this.blobs_url = source["blobs_url"];
	        this.branches_url = source["branches_url"];
	        this.collaborators_url = source["collaborators_url"];
	        this.comments_url = source["comments_url"];
	        this.commits_url = source["commits_url"];
	        this.compare_url = source["compare_url"];
	        this.contents_url = source["contents_url"];
	        this.contributors_url = source["contributors_url"];
	        this.deployments_url = source["deployments_url"];
	        this.downloads_url = source["downloads_url"];
	        this.events_url = source["events_url"];
	        this.forks_url = source["forks_url"];
	        this.git_commits_url = source["git_commits_url"];
	        this.git_refs_url = source["git_refs_url"];
	        this.git_tags_url = source["git_tags_url"];
	        this.hooks_url = source["hooks_url"];
	        this.issue_comment_url = source["issue_comment_url"];
	        this.issue_events_url = source["issue_events_url"];
	        this.issues_url = source["issues_url"];
	        this.keys_url = source["keys_url"];
	        this.labels_url = source["labels_url"];
	        this.languages_url = source["languages_url"];
	        this.merges_url = source["merges_url"];
	        this.milestones_url = source["milestones_url"];
	        this.notifications_url = source["notifications_url"];
	        this.pulls_url = source["pulls_url"];
	        this.releases_url = source["releases_url"];
	        this.stargazers_url = source["stargazers_url"];
	        this.statuses_url = source["statuses_url"];
	        this.subscribers_url = source["subscribers_url"];
	        this.subscription_url = source["subscription_url"];
	        this.tags_url = source["tags_url"];
	        this.trees_url = source["trees_url"];
	        this.teams_url = source["teams_url"];
	        this.text_matches = this.convertValues(source["text_matches"], TextMatch);
	        this.visibility = source["visibility"];
	        this.role_name = source["role_name"];
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

export namespace githubauth {
	
	export class DeviceCode {
	    device_code: string;
	    user_code: string;
	    verification_uri: string;
	    expires_in: number;
	    interval: number;
	
	    static createFrom(source: any = {}) {
	        return new DeviceCode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.device_code = source["device_code"];
	        this.user_code = source["user_code"];
	        this.verification_uri = source["verification_uri"];
	        this.expires_in = source["expires_in"];
	        this.interval = source["interval"];
	    }
	}
	export class User {
	    login: string;
	    name: string;
	    avatar_url: string;
	
	    static createFrom(source: any = {}) {
	        return new User(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.login = source["login"];
	        this.name = source["name"];
	        this.avatar_url = source["avatar_url"];
	    }
	}

}

export namespace issueview {
	
	export class ConflictsInfo {
	    pr_number: number;
	    base: string;
	    head: string;
	    conflicting_files: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConflictsInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pr_number = source["pr_number"];
	        this.base = source["base"];
	        this.head = source["head"];
	        this.conflicting_files = source["conflicting_files"];
	    }
	}
	export class OrphanQuestionInfo {
	    pending_question_id: string;
	    since: number;
	
	    static createFrom(source: any = {}) {
	        return new OrphanQuestionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pending_question_id = source["pending_question_id"];
	        this.since = source["since"];
	    }
	}
	export class TestsFailingInfo {
	    failing_jobs: string[];
	    failing_check_run_urls: string[];
	    head_sha: string;
	    self_heal_attempts?: number;
	    attempt_cap?: number;
	    max_attempts_reached?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TestsFailingInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.failing_jobs = source["failing_jobs"];
	        this.failing_check_run_urls = source["failing_check_run_urls"];
	        this.head_sha = source["head_sha"];
	        this.self_heal_attempts = source["self_heal_attempts"];
	        this.attempt_cap = source["attempt_cap"];
	        this.max_attempts_reached = source["max_attempts_reached"];
	    }
	}
	export class PoolBadge {
	    pool_id: string;
	    name: string;
	    provider: string;
	
	    static createFrom(source: any = {}) {
	        return new PoolBadge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pool_id = source["pool_id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	    }
	}
	export class IssueView {
	    issue: types.Issue;
	    latest_plan?: types.Plan;
	    active_session?: types.Session;
	    paused_session?: types.Session;
	    last_failure?: types.Session;
	    last_session?: types.Session;
	    pool_badge?: PoolBadge;
	    derived_column: string;
	    tests_failing_info?: TestsFailingInfo;
	    conflicts_info?: ConflictsInfo;
	    orphan_question?: OrphanQuestionInfo;
	
	    static createFrom(source: any = {}) {
	        return new IssueView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.issue = this.convertValues(source["issue"], types.Issue);
	        this.latest_plan = this.convertValues(source["latest_plan"], types.Plan);
	        this.active_session = this.convertValues(source["active_session"], types.Session);
	        this.paused_session = this.convertValues(source["paused_session"], types.Session);
	        this.last_failure = this.convertValues(source["last_failure"], types.Session);
	        this.last_session = this.convertValues(source["last_session"], types.Session);
	        this.pool_badge = this.convertValues(source["pool_badge"], PoolBadge);
	        this.derived_column = source["derived_column"];
	        this.tests_failing_info = this.convertValues(source["tests_failing_info"], TestsFailingInfo);
	        this.conflicts_info = this.convertValues(source["conflicts_info"], ConflictsInfo);
	        this.orphan_question = this.convertValues(source["orphan_question"], OrphanQuestionInfo);
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

export namespace logbuffer {
	
	export class Entry {
	    // Go type: time
	    ts: any;
	    level: string;
	    source: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ts = this.convertValues(source["ts"], null);
	        this.level = source["level"];
	        this.source = source["source"];
	        this.text = source["text"];
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
	
	export class AnswerSubmission {
	    workspace_id: string;
	    issue_number: number;
	    revision: number;
	    answers: Record<string, string>;
	    multi: Record<string, Array<string>>;
	
	    static createFrom(source: any = {}) {
	        return new AnswerSubmission(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace_id = source["workspace_id"];
	        this.issue_number = source["issue_number"];
	        this.revision = source["revision"];
	        this.answers = source["answers"];
	        this.multi = source["multi"];
	    }
	}
	export class BundledSkill {
	    name: string;
	    path: string;
	    body: string;
	
	    static createFrom(source: any = {}) {
	        return new BundledSkill(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.body = source["body"];
	    }
	}
	export class CostEstimate {
	    tokens: number;
	    cost_usd: number;
	    has_rate: boolean;
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new CostEstimate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tokens = source["tokens"];
	        this.cost_usd = source["cost_usd"];
	        this.has_rate = source["has_rate"];
	        this.model = source["model"];
	    }
	}
	export class GoalSpendResult {
	    total_usd: number;
	    run_count: number;
	
	    static createFrom(source: any = {}) {
	        return new GoalSpendResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_usd = source["total_usd"];
	        this.run_count = source["run_count"];
	    }
	}
	export class LabelFilterState {
	    labels: string[];
	    mode: string;
	
	    static createFrom(source: any = {}) {
	        return new LabelFilterState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.labels = source["labels"];
	        this.mode = source["mode"];
	    }
	}
	export class NotifyPrefs {
	    muted: boolean;
	    quiet_start: string;
	    quiet_end: string;
	
	    static createFrom(source: any = {}) {
	        return new NotifyPrefs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.muted = source["muted"];
	        this.quiet_start = source["quiet_start"];
	        this.quiet_end = source["quiet_end"];
	    }
	}
	export class ProviderInfo {
	    kind: string;
	    display_name: string;
	    default_endpoint: string;
	    needs_api_key: boolean;
	    can_spawn: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.display_name = source["display_name"];
	        this.default_endpoint = source["default_endpoint"];
	        this.needs_api_key = source["needs_api_key"];
	        this.can_spawn = source["can_spawn"];
	    }
	}
	export class SpawnEstimate {
	    tokens: number;
	    cost_cents: number;
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new SpawnEstimate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tokens = source["tokens"];
	        this.cost_cents = source["cost_cents"];
	        this.model = source["model"];
	    }
	}

}

export namespace store {
	
	export class PendingPlan {
	    workspace_id: string;
	    issue_number: number;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new PendingPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace_id = source["workspace_id"];
	        this.issue_number = source["issue_number"];
	        this.revision = source["revision"];
	    }
	}

}

export namespace types {
	
	export class AutoArchive {
	    enabled: boolean;
	    days_closed: number;
	
	    static createFrom(source: any = {}) {
	        return new AutoArchive(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.days_closed = source["days_closed"];
	    }
	}
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
	export class FileIntent {
	    path: string;
	    intent: string;
	
	    static createFrom(source: any = {}) {
	        return new FileIntent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.intent = source["intent"];
	    }
	}
	export class IssueQuery {
	    labels: string[];
	    milestone: string;
	    free_text: string;
	    includes: number[];
	    excludes: number[];
	
	    static createFrom(source: any = {}) {
	        return new IssueQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.labels = source["labels"];
	        this.milestone = source["milestone"];
	        this.free_text = source["free_text"];
	        this.includes = source["includes"];
	        this.excludes = source["excludes"];
	    }
	}
	export class Goal {
	    id: string;
	    workspace_id: string;
	    title: string;
	    intent: string;
	    acceptance_rule: string;
	    issue_filter: IssueQuery;
	    status: string;
	    order: number;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    achieved_at?: any;
	    notes: string;
	
	    static createFrom(source: any = {}) {
	        return new Goal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspace_id = source["workspace_id"];
	        this.title = source["title"];
	        this.intent = source["intent"];
	        this.acceptance_rule = source["acceptance_rule"];
	        this.issue_filter = this.convertValues(source["issue_filter"], IssueQuery);
	        this.status = source["status"];
	        this.order = source["order"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.achieved_at = this.convertValues(source["achieved_at"], null);
	        this.notes = source["notes"];
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
	export class Question {
	    id: string;
	    type: string;
	    prompt: string;
	    options?: string[];
	    default?: string;
	    required: boolean;
	    answer?: string;
	
	    static createFrom(source: any = {}) {
	        return new Question(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.prompt = source["prompt"];
	        this.options = source["options"];
	        this.default = source["default"];
	        this.required = source["required"];
	        this.answer = source["answer"];
	    }
	}
	export class Plan {
	    issue_number: number;
	    workspace_id: string;
	    revision: number;
	    goal_summary?: string;
	    executive_summary?: string;
	    plan_markdown: string;
	    files_to_modify: FileIntent[];
	    dependencies_detected: number[];
	    suggested_labels?: string[];
	    questions: Question[];
	    estimated_complexity: string;
	    ready_to_execute: boolean;
	    // Go type: time
	    generated_at: any;
	    // Go type: time
	    approved_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.issue_number = source["issue_number"];
	        this.workspace_id = source["workspace_id"];
	        this.revision = source["revision"];
	        this.goal_summary = source["goal_summary"];
	        this.executive_summary = source["executive_summary"];
	        this.plan_markdown = source["plan_markdown"];
	        this.files_to_modify = this.convertValues(source["files_to_modify"], FileIntent);
	        this.dependencies_detected = source["dependencies_detected"];
	        this.suggested_labels = source["suggested_labels"];
	        this.questions = this.convertValues(source["questions"], Question);
	        this.estimated_complexity = source["estimated_complexity"];
	        this.ready_to_execute = source["ready_to_execute"];
	        this.generated_at = this.convertValues(source["generated_at"], null);
	        this.approved_at = this.convertValues(source["approved_at"], null);
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
	export class Issue {
	    number: number;
	    workspace_id: string;
	    title: string;
	    body: string;
	    labels: string[];
	    state: string;
	    url: string;
	    // Go type: time
	    updated_at: any;
	    goal_id?: string;
	    priority: number;
	    dependencies: number[];
	    dep_rationale: string;
	    column: string;
	    plan?: Plan;
	    session_id?: string;
	    last_error?: string;
	    pr_number?: number;
	    pr_url?: string;
	    // Go type: time
	    archived_at?: any;
	    // Go type: time
	    closed_at?: any;
	    waiting_for_pool?: boolean;
	    pipeline_step_id?: string;
	    pipeline_loops?: Record<string, number>;
	    pipeline_version?: number;
	    work_seconds?: number;
	    work_seconds_plan?: number;
	    work_seconds_execute?: number;
	    cost_usd?: number;
	
	    static createFrom(source: any = {}) {
	        return new Issue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.number = source["number"];
	        this.workspace_id = source["workspace_id"];
	        this.title = source["title"];
	        this.body = source["body"];
	        this.labels = source["labels"];
	        this.state = source["state"];
	        this.url = source["url"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.goal_id = source["goal_id"];
	        this.priority = source["priority"];
	        this.dependencies = source["dependencies"];
	        this.dep_rationale = source["dep_rationale"];
	        this.column = source["column"];
	        this.plan = this.convertValues(source["plan"], Plan);
	        this.session_id = source["session_id"];
	        this.last_error = source["last_error"];
	        this.pr_number = source["pr_number"];
	        this.pr_url = source["pr_url"];
	        this.archived_at = this.convertValues(source["archived_at"], null);
	        this.closed_at = this.convertValues(source["closed_at"], null);
	        this.waiting_for_pool = source["waiting_for_pool"];
	        this.pipeline_step_id = source["pipeline_step_id"];
	        this.pipeline_loops = source["pipeline_loops"];
	        this.pipeline_version = source["pipeline_version"];
	        this.work_seconds = source["work_seconds"];
	        this.work_seconds_plan = source["work_seconds_plan"];
	        this.work_seconds_execute = source["work_seconds_execute"];
	        this.cost_usd = source["cost_usd"];
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
	
	export class Label {
	    name: string;
	    color: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new Label(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.color = source["color"];
	        this.description = source["description"];
	    }
	}
	export class MidRunAnswer {
	    question_id: string;
	    answer: string;
	    multi?: string[];
	
	    static createFrom(source: any = {}) {
	        return new MidRunAnswer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.question_id = source["question_id"];
	        this.answer = source["answer"];
	        this.multi = source["multi"];
	    }
	}
	export class SkillRef {
	    path: string;
	    source: string;
	    name: string;
	    display_name: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.source = source["source"];
	        this.name = source["name"];
	        this.display_name = source["display_name"];
	        this.description = source["description"];
	    }
	}
	export class PipelineStep {
	    id: string;
	    name: string;
	    skill_ref: SkillRef;
	    auto_chain: boolean;
	    max_loops: number;
	    on_success: string;
	    on_fail: string;
	
	    static createFrom(source: any = {}) {
	        return new PipelineStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.skill_ref = this.convertValues(source["skill_ref"], SkillRef);
	        this.auto_chain = source["auto_chain"];
	        this.max_loops = source["max_loops"];
	        this.on_success = source["on_success"];
	        this.on_fail = source["on_fail"];
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
	
	export class Pool {
	    id: string;
	    name: string;
	    provider: string;
	    endpoint: string;
	    model: string;
	    capacity: number;
	    enabled: boolean;
	    api_key?: string;
	    role: string;
	    // Go type: time
	    created_at: any;
	    priority: number;
	    temperature?: number;
	    max_turns?: number;
	    max_input_tokens?: number;
	    bash_timeout?: number;
	    output_cap?: number;
	    scope?: string;
	    workspace_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new Pool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.endpoint = source["endpoint"];
	        this.model = source["model"];
	        this.capacity = source["capacity"];
	        this.enabled = source["enabled"];
	        this.api_key = source["api_key"];
	        this.role = source["role"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.priority = source["priority"];
	        this.temperature = source["temperature"];
	        this.max_turns = source["max_turns"];
	        this.max_input_tokens = source["max_input_tokens"];
	        this.bash_timeout = source["bash_timeout"];
	        this.output_cap = source["output_cap"];
	        this.scope = source["scope"];
	        this.workspace_id = source["workspace_id"];
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
	export class PoolUsage {
	    pool_id: string;
	    pool_name: string;
	    provider: string;
	    window: string;
	    limit_value: number;
	    used: number;
	    // Go type: time
	    resets_at: any;
	    // Go type: time
	    captured_at: any;
	
	    static createFrom(source: any = {}) {
	        return new PoolUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pool_id = source["pool_id"];
	        this.pool_name = source["pool_name"];
	        this.provider = source["provider"];
	        this.window = source["window"];
	        this.limit_value = source["limit_value"];
	        this.used = source["used"];
	        this.resets_at = this.convertValues(source["resets_at"], null);
	        this.captured_at = this.convertValues(source["captured_at"], null);
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
	    blocked_reason?: string;
	    pending_question_id?: string;
	    pool_id?: string;
	    acknowledged_at?: number;
	    pipeline_step_name?: string;
	    input_tokens?: number;
	    output_tokens?: number;
	    estimated_cost_cents?: number;
	
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
	        this.blocked_reason = source["blocked_reason"];
	        this.pending_question_id = source["pending_question_id"];
	        this.pool_id = source["pool_id"];
	        this.acknowledged_at = source["acknowledged_at"];
	        this.pipeline_step_name = source["pipeline_step_name"];
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.estimated_cost_cents = source["estimated_cost_cents"];
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
	    auto_apply_labels?: boolean;
	    preferred_plan_pool_id?: string;
	    preferred_work_pool_id?: string;
	    per_stage?: Record<string, SkillRef>;
	    skills_migrated?: boolean;
	    auto_self_heal?: boolean;
	    self_heal_attempt_cap?: number;
	
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
	        this.auto_apply_labels = source["auto_apply_labels"];
	        this.preferred_plan_pool_id = source["preferred_plan_pool_id"];
	        this.preferred_work_pool_id = source["preferred_work_pool_id"];
	        this.per_stage = this.convertValues(source["per_stage"], SkillRef, true);
	        this.skills_migrated = source["skills_migrated"];
	        this.auto_self_heal = source["auto_self_heal"];
	        this.self_heal_attempt_cap = source["self_heal_attempt_cap"];
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
	
	export class WorkspacePipeline {
	    steps: PipelineStep[];
	    version: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkspacePipeline(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.steps = this.convertValues(source["steps"], PipelineStep);
	        this.version = source["version"];
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
	    auto_archive?: AutoArchive;
	    pipeline?: WorkspacePipeline;
	
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
	        this.auto_archive = this.convertValues(source["auto_archive"], AutoArchive);
	        this.pipeline = this.convertValues(source["pipeline"], WorkspacePipeline);
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

export namespace workerpool {
	
	export class PoolStatus {
	    pool: types.Pool;
	    active: number;
	
	    static createFrom(source: any = {}) {
	        return new PoolStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pool = this.convertValues(source["pool"], types.Pool);
	        this.active = source["active"];
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
	    name: string;
	    pass: boolean;
	    info: string;
	
	    static createFrom(source: any = {}) {
	        return new OnboardCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.pass = source["pass"];
	        this.info = source["info"];
	    }
	}
	export class Inspection {
	    repo_path: string;
	    checks: OnboardCheck[];
	    github_owner: string;
	    github_repo: string;
	    default_branch: string;
	    suggested_id: string;
	    skill_profile: types.SkillProfile;
	    conventions: types.ConventionHints;
	
	    static createFrom(source: any = {}) {
	        return new Inspection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo_path = source["repo_path"];
	        this.checks = this.convertValues(source["checks"], OnboardCheck);
	        this.github_owner = source["github_owner"];
	        this.github_repo = source["github_repo"];
	        this.default_branch = source["default_branch"];
	        this.suggested_id = source["suggested_id"];
	        this.skill_profile = this.convertValues(source["skill_profile"], types.SkillProfile);
	        this.conventions = this.convertValues(source["conventions"], types.ConventionHints);
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

