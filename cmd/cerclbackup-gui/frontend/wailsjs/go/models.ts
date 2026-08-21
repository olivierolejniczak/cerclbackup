export namespace api {
	
	export class AuditResult {
	    Checked: number;
	    Valid: number;
	    Corrupted: number;
	    Orphaned: number;
	
	    static createFrom(source: any = {}) {
	        return new AuditResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Checked = source["Checked"];
	        this.Valid = source["Valid"];
	        this.Corrupted = source["Corrupted"];
	        this.Orphaned = source["Orphaned"];
	    }
	}
	export class BackedUpFile {
	    Path: string;
	    FileID: string;
	    Shards: number;
	    Err: string;
	
	    static createFrom(source: any = {}) {
	        return new BackedUpFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.FileID = source["FileID"];
	        this.Shards = source["Shards"];
	        this.Err = source["Err"];
	    }
	}
	export class BackupResult {
	    Scheme: protocol.RSScheme;
	    Files: BackedUpFile[];
	    PrunedVersions: number;
	    PushedToBuddies: number;
	
	    static createFrom(source: any = {}) {
	        return new BackupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Scheme = this.convertValues(source["Scheme"], protocol.RSScheme);
	        this.Files = this.convertValues(source["Files"], BackedUpFile);
	        this.PrunedVersions = source["PrunedVersions"];
	        this.PushedToBuddies = source["PushedToBuddies"];
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
	export class BuddyStatusEntry {
	    Entry?: buddy.Entry;
	    Online: boolean;
	    Latency: number;
	
	    static createFrom(source: any = {}) {
	        return new BuddyStatusEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Entry = this.convertValues(source["Entry"], buddy.Entry);
	        this.Online = source["Online"];
	        this.Latency = source["Latency"];
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
	export class StorageStats {
	    UniquePaths: number;
	    TotalVersions: number;
	    MultiVersion: number;
	    LogicalBytes: number;
	    DiskBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new StorageStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.UniquePaths = source["UniquePaths"];
	        this.TotalVersions = source["TotalVersions"];
	        this.MultiVersion = source["MultiVersion"];
	        this.LogicalBytes = source["LogicalBytes"];
	        this.DiskBytes = source["DiskBytes"];
	    }
	}
	export class DoctorCheck {
	    Name: string;
	    OK: boolean;
	    Msg: string;
	
	    static createFrom(source: any = {}) {
	        return new DoctorCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.OK = source["OK"];
	        this.Msg = source["Msg"];
	    }
	}
	export class DoctorResult {
	    Checks: DoctorCheck[];
	    AllOK: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DoctorResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Checks = this.convertValues(source["Checks"], DoctorCheck);
	        this.AllOK = source["AllOK"];
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
	export class DashboardResult {
	    Status: string;
	    Doctor?: DoctorResult;
	    Buddies: BuddyStatusEntry[];
	    BuddiesTotal: number;
	    BuddiesUp: number;
	    Storage?: StorageStats;
	
	    static createFrom(source: any = {}) {
	        return new DashboardResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Status = source["Status"];
	        this.Doctor = this.convertValues(source["Doctor"], DoctorResult);
	        this.Buddies = this.convertValues(source["Buddies"], BuddyStatusEntry);
	        this.BuddiesTotal = source["BuddiesTotal"];
	        this.BuddiesUp = source["BuddiesUp"];
	        this.Storage = this.convertValues(source["Storage"], StorageStats);
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
	export class DiffChange {
	    Path: string;
	    Version: number;
	    // Go type: time
	    BackedAt: any;
	    FileID: string;
	    Size: number;
	    Kind: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.Version = source["Version"];
	        this.BackedAt = this.convertValues(source["BackedAt"], null);
	        this.FileID = source["FileID"];
	        this.Size = source["Size"];
	        this.Kind = source["Kind"];
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
	
	
	export class ExportResult {
	    OutPath: string;
	    Entry?: protocol.ManifestEntry;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.OutPath = source["OutPath"];
	        this.Entry = this.convertValues(source["Entry"], protocol.ManifestEntry);
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
	export class ImportResult {
	    Entry?: protocol.ManifestEntry;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Entry = this.convertValues(source["Entry"], protocol.ManifestEntry);
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
	export class InitParams {
	    Password: string;
	    StoreDir: string;
	    Force: boolean;
	
	    static createFrom(source: any = {}) {
	        return new InitParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Password = source["Password"];
	        this.StoreDir = source["StoreDir"];
	        this.Force = source["Force"];
	    }
	}
	export class InitResult {
	    PeerID: string;
	    RecoveryPhrase: string;
	    KeystorePath: string;
	    StoreDir: string;
	
	    static createFrom(source: any = {}) {
	        return new InitResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.PeerID = source["PeerID"];
	        this.RecoveryPhrase = source["RecoveryPhrase"];
	        this.KeystorePath = source["KeystorePath"];
	        this.StoreDir = source["StoreDir"];
	    }
	}
	export class InviteEmailResult {
	    PeerID: string;
	    Words: string;
	    PayloadJSON: number[];
	    Subject: string;
	    Body: string;
	
	    static createFrom(source: any = {}) {
	        return new InviteEmailResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.PeerID = source["PeerID"];
	        this.Words = source["Words"];
	        this.PayloadJSON = source["PayloadJSON"];
	        this.Subject = source["Subject"];
	        this.Body = source["Body"];
	    }
	}
	export class InviteResult {
	    Words: string;
	    VerbalWords: string;
	    Addrs: string[];
	    JoinAddr: string;
	    PeerID: string;
	
	    static createFrom(source: any = {}) {
	        return new InviteResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Words = source["Words"];
	        this.VerbalWords = source["VerbalWords"];
	        this.Addrs = source["Addrs"];
	        this.JoinAddr = source["JoinAddr"];
	        this.PeerID = source["PeerID"];
	    }
	}
	export class ManifestPullResult {
	    Path: string;
	    Bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new ManifestPullResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.Bytes = source["Bytes"];
	    }
	}
	export class PruneResult {
	    PrunedIDs: string[];
	    Deleted: number;
	
	    static createFrom(source: any = {}) {
	        return new PruneResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.PrunedIDs = source["PrunedIDs"];
	        this.Deleted = source["Deleted"];
	    }
	}
	export class RecoverResult {
	    PeerID: string;
	
	    static createFrom(source: any = {}) {
	        return new RecoverResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.PeerID = source["PeerID"];
	    }
	}
	export class RestoreResult {
	    Entry?: protocol.ManifestEntry;
	    IntegrityPassed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RestoreResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Entry = this.convertValues(source["Entry"], protocol.ManifestEntry);
	        this.IntegrityPassed = source["IntegrityPassed"];
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

export namespace buddy {
	
	export class Entry {
	    peer_id: string;
	    pub_key: number[];
	    friendly_name?: string;
	    addrs?: string[];
	    // Go type: time
	    added_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.peer_id = source["peer_id"];
	        this.pub_key = source["pub_key"];
	        this.friendly_name = source["friendly_name"];
	        this.addrs = source["addrs"];
	        this.added_at = this.convertValues(source["added_at"], null);
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

export namespace circle {
	
	export class Circle {
	    id: string;
	    name: string;
	    salt: number[];
	    scheme: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Circle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.salt = source["salt"];
	        this.scheme = source["scheme"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	
	export class ConfigShowResult {
	    Config: any;
	    Path: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigShowResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Config = source["Config"];
	        this.Path = source["Path"];
	    }
	}
	export class JoinEmailResult {
	    Circle: string;
	    PeerID: string;
	
	    static createFrom(source: any = {}) {
	        return new JoinEmailResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Circle = source["Circle"];
	        this.PeerID = source["PeerID"];
	    }
	}
	export class ServeStatus {
	    Running: boolean;
	    PeerID: string;
	    Addrs: string[];
	
	    static createFrom(source: any = {}) {
	        return new ServeStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Running = source["Running"];
	        this.PeerID = source["PeerID"];
	        this.Addrs = source["Addrs"];
	    }
	}

}

export namespace protocol {
	
	export class ShardLocation {
	    shard_index: number;
	    is_parity: boolean;
	    buddy_id: string;
	    storage_key: string;
	
	    static createFrom(source: any = {}) {
	        return new ShardLocation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shard_index = source["shard_index"];
	        this.is_parity = source["is_parity"];
	        this.buddy_id = source["buddy_id"];
	        this.storage_key = source["storage_key"];
	    }
	}
	export class RSScheme {
	    DataShards: number;
	    ParityShards: number;
	
	    static createFrom(source: any = {}) {
	        return new RSScheme(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DataShards = source["DataShards"];
	        this.ParityShards = source["ParityShards"];
	    }
	}
	export class ManifestEntry {
	    file_id: string;
	    path: string;
	    hash: string;
	    size: number;
	    // Go type: time
	    modified: any;
	    scheme: RSScheme;
	    shards: ShardLocation[];
	    version?: number;
	    // Go type: time
	    backed_at?: any;
	    compressed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ManifestEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_id = source["file_id"];
	        this.path = source["path"];
	        this.hash = source["hash"];
	        this.size = source["size"];
	        this.modified = this.convertValues(source["modified"], null);
	        this.scheme = this.convertValues(source["scheme"], RSScheme);
	        this.shards = this.convertValues(source["shards"], ShardLocation);
	        this.version = source["version"];
	        this.backed_at = this.convertValues(source["backed_at"], null);
	        this.compressed = source["compressed"];
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

export namespace rebalance {
	
	export class Result {
	    FilesProcessed: number;
	    ShardsAttempted: number;
	    ShardsOK: number;
	    Errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.FilesProcessed = source["FilesProcessed"];
	        this.ShardsAttempted = source["ShardsAttempted"];
	        this.ShardsOK = source["ShardsOK"];
	        this.Errors = source["Errors"];
	    }
	}

}

export namespace scrub {
	
	export class Report {
	    Checked: number;
	    OK: number;
	    Corrupted: number;
	    Revived: number;
	    Failed: number;
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Checked = source["Checked"];
	        this.OK = source["OK"];
	        this.Corrupted = source["Corrupted"];
	        this.Revived = source["Revived"];
	        this.Failed = source["Failed"];
	    }
	}

}

