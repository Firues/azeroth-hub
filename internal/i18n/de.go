package i18n

// dictDe is German, du-Form — the register German game tools use.
// Durations are phrased so the number keeps its nominative ("ist 3 Tage her")
// instead of fighting German case after "vor".
var dictDe = map[string]string{
	// --- health.go ---------------------------------------------------------
	"health.mysql.process.label": "MySQL: Prozess",
	"health.mysql.port.label":    "MySQL: Port",
	"health.auth.port.label":     "Authserver: Port",
	"health.world.process.label": "Worldserver: Prozess",
	"health.repack.label":        "Repack vorhanden",
	"health.repack.noroot":       "Stammverzeichnis des Repacks nicht gefunden",
	"health.path.missing":        "%s (nicht gefunden)",
	"health.repack.absent":       "es fehlt: %s",
	"health.port.probefail":      "Abfrage fehlgeschlagen: %v",
	"health.port.notrunning":     "läuft nicht",
	"health.process.count":       "Prozesse: %d (",
	"health.process.portowner":   ", Port wird von PID %d gehalten",
	"health.port.listening":      "Port %d lauscht",
	"health.port.noanswer":       "Port %d antwortet nicht",
	"health.ping.label":          "MySQL: antwortet",
	"health.check.unconfigured":  "Prüfung nicht konfiguriert",
	"health.ping.foreign":        "Port %d wird von einem fremden Prozess gehalten — er antwortet auf TCP, aber nicht als MySQL: %v",
	"health.ping.ms":             "Antwort in %d ms",

	// --- supervisor.go -----------------------------------------------------
	"err.busy":           "eine Operation läuft bereits — warte, bis die aktuelle fertig ist",
	"process.ownerless":  "kein Port-Besitzer, niedrigste PID genommen",
	"process.owner":      "Port-Besitzer genommen",
	"process.pids":       "%d Prozesse: %s; %s %d",
	"process.exited":     "der Prozess hat sich selbst beendet",
	"process.exited.err": "der Prozess hat sich selbst beendet: %v",

	// --- stop.go -----------------------------------------------------------
	"stop.denied":             "%s: Stopp abgebrochen — stoppe zuerst %s, sonst würde %s gegen eine tote Datenbank weiterlaufen",
	"stop.unknown":            "%s: unbekannter Dienst",
	"stop.kill.fail":          "%s (PID %d): der Prozess konnte nicht getötet werden",
	"stop.kill.alive":         "%s (PID %d): der Prozess lebt selbst nach kill noch",
	"stop.confirm.declined":   "Stopp vom Benutzer abgebrochen: der Prozess wurde nicht angetastet",
	"stop.confirm.unanswered": "der Bestätigungsdialog blieb unbeantwortet: der Prozess wurde nicht angetastet",
	"stop.mysql.noadmin":      "mysql: mysqladmin.exe im Repack-Layout nicht gefunden",
	"stop.mysql.adminfail":    "mysql: mysqladmin shutdown fehlgeschlagen",
	"stop.mysql.alive":        "mysql: Prozess %d lebt nach shutdown noch (Port %d hat sich geschlossen, aber mysqld ist nicht gegangen)",

	// --- start.go ----------------------------------------------------------
	"start.probe.fail":      "%s: konnte nicht geprüft werden, ob er schon läuft",
	"start.rollback.word":   "Rollback",
	"start.world.needsdb":   "world: starte zuerst MySQL — ohne Datenbank startet worldserver nicht",
	"start.port.timeout":    "%s: Port %d hat nicht innerhalb von %s geöffnet",
	"start.db.unconfigured": "DB: Prüfung nicht konfiguriert",
	"start.db.foreign":      "DB: Port %d wird von einem fremden Prozess gehalten - er antwortet auf TCP, aber nicht als MySQL (%v)",
	"start.db.prefix":       "DB",

	// --- worldlog.go -------------------------------------------------------
	"worldlog.partial": "die Welt wurde ohne ihren Speicherzyklus gestoppt: die Datenbank-Pools haben sich geschlossen, " +
		"aber der Befehl `Server shutdown in N second(s)` lief nie, daher könnten Spieler in der Welt nicht gespeichert sein. " +
		"Die Daten können bis zu PlayerSaveInterval hinterherhinken (standardmäßig 15 Minuten). " +
		"Stoppe die Welt das nächste Mal mit Ctrl+C in ihrem eigenen Fenster — nur dieser Weg löst das Speichern aus.",
	"worldlog.unconfirmed": "die Stopp-Reihenfolge der Welt ließ sich nicht aus dem Log bestätigen; prüfe logs\\Server_*.log",

	// --- dumps.go ----------------------------------------------------------
	"dump.cancelled":       "die Erstellung der Kopie wurde abgebrochen",
	"dump.create.fail":     "%s konnte nicht erstellt werden",
	"dump.write.fail":      "%s konnte nicht geschrieben werden",
	"dump.fail.msg":        "der Dump von %s ist fehlgeschlagen: %s",
	"dump.fail":            "der Dump von %s ist fehlgeschlagen",
	"dump.read.fail":       "%s konnte nicht gelesen werden",
	"dump.phys.empty":      "%s ist leer — kein Archiv wurde erstellt",
	"dump.norepack":        "Repack nicht gefunden: es ist unbekannt, wo die Datenbanken und der backups-Ordner liegen",
	"dump.nomysqldump":     "mysqldump nicht gefunden: %s",
	"dump.nopassconf":      "%s nicht gefunden — ohne ihn ist das Datenbank-Passwort unbekannt",
	"dump.backupsdir.fail": "der backups-Ordner ist nicht zugänglich",
	"dump.data.missing":    "mysql\\data im Repack nicht gefunden",
	"dump.mysql.required":  "MySQL antwortet nicht auf Port 3306 — Dumps werden von einem laufenden MySQL genommen",
	"dump.mysql.off":       "MySQL läuft: ein Snapshot wird nur bei gestoppter Datenbank genommen, sonst wäre die Kopie inkonsistent",
	"dump.world.off":       "worldserver läuft: stoppe zuerst den Server, dann die Datenbank",
	"dump.names.spent":     "%s hat schon hundert Namen für %s belegt",
	"dump.tail.read.fail":  "der Dump %s konnte nicht gelesen werden",
	"dump.empty":           "der Dump %s ist leer",
	"dump.tail.end.fail":   "das Ende des Dumps %s konnte nicht gelesen werden",
	"dump.truncated":       "der Dump %s ist abgeschnitten: die letzte Zeile ist nicht %q — die Datei ist unvollständig und darf nicht wiederhergestellt werden",

	// --- restore.go --------------------------------------------------------
	"restore.norepack":    "Repack nicht gefunden: es ist unbekannt, wohin die Kopie entpackt werden soll",
	"restore.nofile":      "der Ordner %s hat keine Datei %s",
	"restore.empty":       "%s ist leer — die Kopie taugt nichts",
	"restore.step.stop":   "der Server wird gestoppt",
	"restore.unavailable": "die Wiederherstellung ist nicht verfügbar: der Server ist nicht unter der Kontrolle des Panels",
	"restore.notstopped":  "der Server hat nicht gestoppt, die Kopie wurde nicht angetastet",
	"restore.step.safety": "die Sicherheitskopie wird erstellt",
	"restore.safety.fail": "die Sicherheitskopie ist fehlgeschlagen, das Rollback wurde abgebrochen",
	"restore.step.unpack": "die Kopie wird entpackt",
	"restore.step.done":   "fertig",
	"restore.noenv":       "die Wiederherstellung ist nicht verfügbar: die Umgebung ist nicht zugänglich",
	"restore.unpack.fail": "das Entpacken ist fehlgeschlagen",

	// --- backups.go --------------------------------------------------------
	"backups.nodir":    "der Ordner mit den Kopien ist unbekannt",
	"backups.nopoint":  "kein Kopien-Ordner angegeben",
	"backups.nofolder": "backups\\ hat keinen Ordner %s",
	"backups.isfile":   "%s ist eine Datei, kein Kopien-Ordner",
	"backups.badname":  "der Ordnername %q ist nicht gültig: ein Ordnername innerhalb von backups\\ wird erwartet",
	"backups.notpoint": "sieht nicht wie ein Rollback-Punkt aus",
	"backups.nodump":   "kein Dump: %s",

	// --- realmdb.go --------------------------------------------------------
	"rdb.nodb":     "keine Datenbankverbindung",
	"rdb.sizes":    "Datenbankgrößen",
	"rdb.chars":    "die Charakterliste",
	"rdb.accounts": "die Kontoliste",

	// --- prune.go, dbping.go, logx.go ---------------------------------------
	"prune.norepack": "Repack nicht gefunden: es ist unbekannt, wo die Kopien liegen",
	"prune.rm.fail":  "%s konnte nicht gelöscht werden",
	"dbping.nopass":  "das MySQL-Passwort ist leer — prüfe temp\\mysql_ac.conf",
	"dbping.open":    "Öffnen der Datenbank (%s)",
	"logx.mkdir":     "Erstellen des Log-Verzeichnisses %s",
	"logx.open":      "Öffnen des Logs %s",

	// --- osops -------------------------------------------------------------
	"console.noconsole": "der Prozess hat keine Konsole",
	"console.denied":    "Zugriff verweigert — der Server läuft als Administrator und das Panel nicht (oder umgekehrt); starte beide mit denselben Rechten",
	"console.badhandle": "ungültiges Handle — der Prozess ist bereits beendet",
	"console.nowindow":  "der Prozess hat keine Konsole (mit CREATE_NO_WINDOW gestartet)",
	"console.code":      "Code %d — die Konsole ist nicht zugänglich",
	"console.conin":     "Öffnen von CONIN$ für PID %d",
	"console.write":     "WriteConsoleInput für PID %d",
	"console.wrote":     "WriteConsoleInput für PID %d: %d von %d Ereignissen geschrieben",
	"console.ctrlevent": "GenerateConsoleCtrlEvent für PID %d",
	"ops.stdin":         "stdin für %s",
	"ops.launch":        "%s wird gestartet",
	"ops.tasklist":      "Auswertung der tasklist-Ausgabe",
	"ops.pslist":        "Snapshot der Prozessliste wird genommen",
	"ops.taskkill":      "taskkill für PID %d",
	"ops.open":          "%s öffnen",

	// --- repack ------------------------------------------------------------
	"layout.selfpath":  "der Pfad zu mir",
	"layout.emptypath": "der Suchpfad ist leer",
	"layout.abs":       "absoluter Pfad für %q",
	"layout.almost":    "sieht wie ein Repack aus, aber es fehlt: %v (Verzeichnis %s)",
	"layout.notfound":  "Repack-Wurzel nicht gefunden: von %s aufwärts wurde keins von %v gefunden",
	"mycnf.nopass":     "%s: der Schlüssel password ist leer",
	"mycnf.read":       "Lesen von %s",
	"mycnf.noclient":   "%s: der Abschnitt [client] wurde nicht gefunden",

	// --- settings.go ---------------------------------------------------------
	"settings.prefix":     "Einstellungen",
	"settings.unreadable": "Einstellungen: die Datei %s kann nicht gelesen werden",
	"settings.nowhere":    "Einstellungen: es ist unbekannt, wohin gespeichert werden soll",

	// --- app.go ---------------------------------------------------------------
	"app.backup.norepack":    "Repack nicht gefunden — der backups-Ordner ist unbekannt",
	"app.backup.unavailable": "das Erstellen von Kopien ist nicht verfügbar: Repack nicht gefunden",
	"app.backup.busy":        "eine Kopie wird bereits erstellt — warte, bis sie fertig ist",
	"app.restore.norepack":   "die Wiederherstellung ist nicht verfügbar: Repack nicht gefunden",
	"app.ops.busy":           "eine andere Kopier-Operation läuft — warte, bis sie fertig ist",
	"app.safety.nomysqldump": "die Sicherheitskopie ist nicht verfügbar: kein mysqldump",
	"app.err.kind":           "unbekannter Kopientyp %q (logical oder physical erwartet)",
	"app.err.service":        "unbekannter Dienst %q (mysql, auth oder world erwartet)",
	"app.restart.stopfail":   "Neustart von %s: der Stopp ist fehlgeschlagen",
	"app.folder.unknown":     "unbekannter Ordner %q (backups, logs, configs oder root erwartet)",
	"app.folder.norepack":    "Repack nicht gefunden — es ist unbekannt, welcher Ordner geöffnet werden soll",
	"app.folder.noenv":       "nichts zu öffnen: die Umgebung ist nicht zugänglich",
	"app.err.lang":           "unbekannte Sprache %q (eine aus: %s erwartet)",

	// --- setup checklist (app.go) ----------------------------------------------
	"setup.repack.label":    "Repack vorhanden",
	"setup.repack.nofolder": "der Repack-Ordner wurde nicht gefunden",
	"setup.repack.fix":      "lege wow-hub.exe in den Repack-Ordner und starte neu",
	"setup.repack.missing":  "es fehlt: %s",
	"setup.repack.fix2":     "entpacke den Repack vollständig",
	"setup.db.label":        "Die Datenbank antwortet",
	"setup.db.notread":      "die Daten wurden noch nicht gelesen",
	"setup.db.fix":          "starte MySQL",
	"setup.db.detail":       "%d Datenbanken, %s, %d Konten",
	"setup.rollback.label":  "Es gibt eine frische Kopie",
	"setup.rollback.none":   "es gibt keine Kopien",
	"setup.rollback.fix":    "drücke auf dem Bildschirm Kopien auf Erstellen",
	"setup.rollback.detail": "die letzte ist %s her (%s)",
	"setup.rollback.stale":  "mache eine neue Kopie: die letzte ist älter als %s",
	"setup.space.label":     "Es ist genug Speicherplatz da",
	"setup.space.unknown":   "der freie Speicher konnte nicht ermittelt werden",
	"setup.space.fix":       "prüfe, dass der backups-Ordner existiert und zugänglich ist",
	"setup.space.detail":    "%s frei",
	"setup.space.lowfix":    "eine Kopie braucht etwa 500 MB — schaffe Platz",
	"setup.server.label":    "Der Server läuft",
	"setup.server.detail":   "%d von %d laufen",
	"setup.server.fix":      "drücke Server starten",

	// --- durations, units (app.go) ----------------------------------------------
	"duration.ltmin":   "weniger als eine Minute",
	"dur.minute.one":   "Minute",
	"dur.minute.few":   "Minuten",
	"dur.minute.many":  "Minuten",
	"dur.minute.other": "Minuten",
	"dur.hour.one":     "Stunde",
	"dur.hour.few":     "Stunden",
	"dur.hour.many":    "Stunden",
	"dur.hour.other":   "Stunden",
	"dur.day.one":      "Tag",
	"dur.day.few":      "Tage",
	"dur.day.many":     "Tage",
	"dur.day.other":    "Tage",
	"unit.b":           "B",
	"unit.kb":          "KB",
	"unit.mb":          "MB",
	"unit.gb":          "GB",
	"unit.tb":          "TB",

	// --- main.go ------------------------------------------------------------
	"tray.open":      "Panel öffnen",
	"tray.quit":      "Beenden",
	"fatal.title":    "Wow Hub — Startfehler",
	"fatal.frontend": "das eingebaute Frontend konnte nicht geöffnet werden",
	"fatal.window":   "das Fenster hat sich nicht geöffnet",
}

func init() { register(De, dictDe) }
