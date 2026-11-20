package i18n

// dictEn is the reference dictionary: every key the backend uses, in English.
//
// It is the fallback for every other language, so it must be complete. The
// parity test (i18n_test.go) fails a build where a language drifts out of
// sync with this set.
var dictEn = map[string]string{
	// --- health.go ---------------------------------------------------------
	"health.mysql.process.label": "MySQL: process",
	"health.mysql.port.label":    "MySQL: port",
	"health.auth.port.label":     "Authserver: port",
	"health.world.process.label": "Worldserver: process",
	"health.repack.label":        "Repack in place",
	"health.repack.noroot":       "repack root not found",
	"health.path.missing":        "%s (not found)",
	"health.repack.absent":       "missing: %s",
	"health.port.probefail":      "could not probe: %v",
	"health.port.notrunning":     "not running",
	"health.process.count":       "processes: %d (",
	"health.process.portowner":   ", port held by PID %d",
	"health.port.listening":      "port %d is listening",
	"health.port.noanswer":       "port %d is not responding",
	"health.ping.label":          "MySQL: answering",
	"health.check.unconfigured":  "check not configured",
	"health.ping.foreign":        "port %d is held by a foreign process — it answers TCP, but not as MySQL: %v",
	"health.ping.ms":             "answered in %d ms",

	// --- supervisor.go -----------------------------------------------------
	"err.busy":           "an operation is already running — wait for the current one to finish",
	"process.ownerless":  "no port owner, took the lowest PID",
	"process.owner":      "took the port owner",
	"process.pids":       "%d processes: %s; %s %d",
	"process.exited":     "the process exited on its own",
	"process.exited.err": "the process exited on its own: %v",

	// --- stop.go -----------------------------------------------------------
	"stop.denied":             "%s: stop cancelled — stop %s first, or %s would keep running against a dead database",
	"stop.unknown":            "%s: unknown service",
	"stop.kill.fail":          "%s (PID %d): failed to kill the process",
	"stop.kill.alive":         "%s (PID %d): the process is alive even after kill",
	"stop.confirm.declined":   "stop cancelled by the user: the process was not touched",
	"stop.confirm.unanswered": "the confirmation dialog was never answered: the process was not touched",
	"stop.mysql.noadmin":      "mysql: mysqladmin.exe not found in the repack layout",
	"stop.mysql.adminfail":    "mysql: mysqladmin shutdown failed",
	"stop.mysql.alive":        "mysql: process %d is alive after shutdown (port %d closed but mysqld did not go away)",

	// --- start.go ----------------------------------------------------------
	"start.probe.fail":      "%s: could not check whether it is already running",
	"start.rollback.word":   "rollback",
	"start.world.needsdb":   "world: start MySQL first — without a database worldserver will not start",
	"start.port.timeout":    "%s: port %d did not open within %s",
	"start.db.unconfigured": "DB: check not configured",
	"start.db.foreign":      "DB: port %d is held by a foreign process - it answers TCP, but not as MySQL (%v)",
	"start.db.prefix":       "DB",

	// --- worldlog.go -------------------------------------------------------
	"worldlog.partial":     "the world was stopped without its save loop: the database pools closed, but the `Server shutdown in N second(s)` command never ran, so players in the world may not have been saved. Data can lag behind up to PlayerSaveInterval (15 minutes by default). Next time, stop the world with Ctrl+C in its own console — that is the only path that triggers saving.",
	"worldlog.unconfirmed": "could not confirm the world's shutdown order from the log; check logs\\Server_*.log",

	// --- dumps.go ----------------------------------------------------------
	"dump.cancelled":       "backup creation was cancelled",
	"dump.create.fail":     "could not create %s",
	"dump.write.fail":      "could not write %s",
	"dump.fail.msg":        "the dump of %s failed: %s",
	"dump.fail":            "the dump of %s failed",
	"dump.read.fail":       "could not read %s",
	"dump.phys.empty":      "%s is empty — no archive was created",
	"dump.norepack":        "repack not found: there is no knowing where the databases and the backups folder are",
	"dump.nomysqldump":     "mysqldump not found: %s",
	"dump.nopassconf":      "%s not found — without it the database password is unknown",
	"dump.backupsdir.fail": "the backups folder is not accessible",
	"dump.data.missing":    "mysql\\data not found in the repack",
	"dump.mysql.required":  "MySQL is not answering on port 3306 — dumps are taken from a running MySQL",
	"dump.mysql.off":       "MySQL is running: a snapshot is taken only with the database stopped, otherwise the copy would be inconsistent",
	"dump.world.off":       "worldserver is running: stop the server first, then the database",
	"dump.names.spent":     "%s already has a hundred names taken for %s",
	"dump.tail.read.fail":  "could not read the dump %s",
	"dump.empty":           "the dump %s is empty",
	"dump.tail.end.fail":   "could not read the end of the dump %s",
	"dump.truncated":       "the dump %s is cut short: the last line is not %q — the file is incomplete and must not be restored",

	// --- restore.go --------------------------------------------------------
	"restore.norepack":    "repack not found: there is no knowing where to unpack the copy",
	"restore.nofile":      "the folder %s has no file %s",
	"restore.empty":       "%s is empty — the copy is no good",
	"restore.step.stop":   "stopping the server",
	"restore.unavailable": "restore is unavailable: the server is not under the panel's control",
	"restore.notstopped":  "the server did not stop, the copy was not touched",
	"restore.step.safety": "making the safety copy",
	"restore.safety.fail": "the safety copy failed, the rollback was cancelled",
	"restore.step.unpack": "unpacking the copy",
	"restore.step.done":   "done",
	"restore.noenv":       "restore is unavailable: the environment is not accessible",
	"restore.unpack.fail": "unpacking failed",

	// --- backups.go --------------------------------------------------------
	"backups.nodir":    "the copies folder is unknown",
	"backups.nopoint":  "no copy folder is given",
	"backups.nofolder": "backups\\ has no folder %s",
	"backups.isfile":   "%s is a file, not a copy folder",
	"backups.badname":  "the folder name %q is not valid: a folder name inside backups\\ is expected",
	"backups.notpoint": "does not look like a rollback point",
	"backups.nodump":   "no dump: %s",

	// --- realmdb.go --------------------------------------------------------
	"rdb.nodb":     "no database connection",
	"rdb.sizes":    "database sizes",
	"rdb.chars":    "the character list",
	"rdb.accounts": "the account list",

	// --- prune.go, dbping.go, logx.go ---------------------------------------
	"prune.norepack": "repack not found: there is no knowing where the copies are",
	"prune.rm.fail":  "could not delete %s",
	"dbping.nopass":  "the MySQL password is empty — check temp\\mysql_ac.conf",
	"dbping.open":    "opening the database (%s)",
	"logx.mkdir":     "creating the log directory %s",
	"logx.open":      "opening the log %s",

	// --- osops -------------------------------------------------------------
	"console.noconsole": "the process has no console",
	"console.denied":    "access denied — the server runs as administrator and the panel does not (or the other way around); run both with the same rights",
	"console.badhandle": "invalid handle — the process has already exited",
	"console.nowindow":  "the process has no console (started with CREATE_NO_WINDOW)",
	"console.code":      "code %d — the console is not accessible",
	"console.conin":     "opening CONIN$ for PID %d",
	"console.write":     "WriteConsoleInput for PID %d",
	"console.wrote":     "WriteConsoleInput for PID %d: wrote %d of %d events",
	"console.ctrlevent": "GenerateConsoleCtrlEvent for PID %d",
	"ops.stdin":         "stdin for %s",
	"ops.launch":        "starting %s",
	"ops.tasklist":      "parsing tasklist output",
	"ops.pslist":        "taking the process list snapshot",
	"ops.taskkill":      "taskkill for PID %d",
	"ops.open":          "open %s",

	// --- repack ------------------------------------------------------------
	"layout.selfpath":  "the path to myself",
	"layout.emptypath": "the search path is empty",
	"layout.abs":       "absolute path for %q",
	"layout.almost":    "looks like a repack, but is missing: %v (directory %s)",
	"layout.notfound":  "repack root not found: walking up from %s none of %v was met",
	"mycnf.nopass":     "%s: the password key is empty",
	"mycnf.read":       "reading %s",
	"mycnf.noclient":   "%s: the [client] section was not found",

	// --- settings.go ---------------------------------------------------------
	"settings.prefix":     "settings",
	"settings.unreadable": "settings: the file %s cannot be read",
	"settings.nowhere":    "settings: no idea where to save",

	// --- app.go ---------------------------------------------------------------
	"app.backup.norepack":    "repack not found — the backups folder is unknown",
	"app.backup.unavailable": "backup creation is unavailable: repack not found",
	"app.backup.busy":        "a copy is already being made — wait for it to finish",
	"app.restore.norepack":   "restore is unavailable: repack not found",
	"app.ops.busy":           "another copy operation is running — wait for it to finish",
	"app.safety.nomysqldump": "the safety copy is unavailable: no mysqldump",
	"app.err.kind":           "unknown copy kind %q (logical or physical expected)",
	"app.err.service":        "unknown service %q (mysql, auth or world expected)",
	"app.restart.stopfail":   "restarting %s: the stop failed",
	"app.folder.unknown":     "unknown folder %q (backups, logs, configs or root expected)",
	"app.folder.norepack":    "repack not found — no knowing which folder to open",
	"app.folder.noenv":       "nothing to open: the environment is not accessible",
	"app.err.lang":           "unknown language %q (one of: %s expected)",

	// --- setup checklist (app.go) ----------------------------------------------
	"setup.repack.label":    "Repack in place",
	"setup.repack.nofolder": "the repack folder was not found",
	"setup.repack.fix":      "put wow-hub.exe into the repack folder and start again",
	"setup.repack.missing":  "missing: %s",
	"setup.repack.fix2":     "unpack the repack completely",
	"setup.db.label":        "The database answers",
	"setup.db.notread":      "the data has not been read yet",
	"setup.db.fix":          "start MySQL",
	"setup.db.detail":       "%d databases, %s, %d accounts",
	"setup.rollback.label":  "A fresh copy exists",
	"setup.rollback.none":   "there are no copies",
	"setup.rollback.fix":    "press Create on the Backups screen",
	"setup.rollback.detail": "the latest one was made %s ago (%s)",
	"setup.rollback.stale":  "make a new copy: the latest one is older than %s",
	"setup.space.label":     "There is enough disk space",
	"setup.space.unknown":   "could not learn the free space",
	"setup.space.fix":       "check that the backups folder exists and is accessible",
	"setup.space.detail":    "%s free",
	"setup.space.lowfix":    "a copy takes about 500 MB — free up some space",
	"setup.server.label":    "The server is running",
	"setup.server.detail":   "%d of %d are running",
	"setup.server.fix":      "press Start the server",

	// --- durations, units (app.go) ----------------------------------------------
	// Every language carries all four plural forms so the parity test can
	// compare key sets exactly; the forms a language does not need repeat the
	// one it does.
	"duration.ltmin":   "less than a minute",
	"dur.minute.one":   "minute",
	"dur.minute.few":   "minutes",
	"dur.minute.many":  "minutes",
	"dur.minute.other": "minutes",
	"dur.hour.one":     "hour",
	"dur.hour.few":     "hours",
	"dur.hour.many":    "hours",
	"dur.hour.other":   "hours",
	"dur.day.one":      "day",
	"dur.day.few":      "days",
	"dur.day.many":     "days",
	"dur.day.other":    "days",
	"unit.b":           "B",
	"unit.kb":          "KB",
	"unit.mb":          "MB",
	"unit.gb":          "GB",
	"unit.tb":          "TB",

	// --- main.go ------------------------------------------------------------
	"tray.open":      "Open the panel",
	"tray.quit":      "Quit",
	"fatal.title":    "Wow Hub — startup error",
	"fatal.frontend": "could not open the embedded frontend",
	"fatal.window":   "the window did not open",
}

func init() { register(En, dictEn) }
