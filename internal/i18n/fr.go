package i18n

// dictFr is French, tutoiement — the register French game launchers use.
var dictFr = map[string]string{
	// --- health.go ---------------------------------------------------------
	"health.mysql.process.label": "MySQL : processus",
	"health.mysql.port.label":    "MySQL : port",
	"health.auth.port.label":     "Authserver : port",
	"health.world.process.label": "Worldserver : processus",
	"health.repack.label":        "Repack présent",
	"health.repack.noroot":       "racine du repack introuvable",
	"health.path.missing":        "%s (introuvable)",
	"health.repack.absent":       "manque : %s",
	"health.port.probefail":      "impossible de sonder : %v",
	"health.port.notrunning":     "ne tourne pas",
	"health.process.count":       "processus : %d (",
	"health.process.portowner":   ", port détenu par le PID %d",
	"health.port.listening":      "le port %d écoute",
	"health.port.noanswer":       "le port %d ne répond pas",
	"health.ping.label":          "MySQL : répond",
	"health.check.unconfigured":  "vérification non configurée",
	"health.ping.foreign":        "le port %d est occupé par un processus étranger — il répond en TCP, mais pas comme MySQL : %v",
	"health.ping.ms":             "réponse en %d ms",

	// --- supervisor.go -----------------------------------------------------
	"err.busy":           "une opération est déjà en cours — attends la fin de celle-ci",
	"process.ownerless":  "pas de propriétaire du port, plus petit PID pris",
	"process.owner":      "propriétaire du port pris",
	"process.pids":       "%d processus : %s ; %s %d",
	"process.exited":     "le processus s'est terminé de lui-même",
	"process.exited.err": "le processus s'est terminé de lui-même : %v",

	// --- stop.go -----------------------------------------------------------
	"stop.denied":             "%s : arrêt annulé — arrête d'abord %s, sinon %s continuerait de tourner sur une base morte",
	"stop.unknown":            "%s : service inconnu",
	"stop.kill.fail":          "%s (PID %d) : impossible de tuer le processus",
	"stop.kill.alive":         "%s (PID %d) : le processus reste vivant même après kill",
	"stop.confirm.declined":   "arrêt annulé par l'utilisateur : le processus n'a pas été touché",
	"stop.confirm.unanswered": "le dialogue de confirmation n'a jamais reçu de réponse : le processus n'a pas été touché",
	"stop.mysql.noadmin":      "mysql : mysqladmin.exe introuvable dans l'arborescence du repack",
	"stop.mysql.adminfail":    "mysql : mysqladmin shutdown a échoué",
	"stop.mysql.alive":        "mysql : le processus %d reste vivant après shutdown (le port %d s'est fermé, mais mysqld n'est pas parti)",

	// --- start.go ----------------------------------------------------------
	"start.probe.fail":      "%s : impossible de vérifier s'il tourne déjà",
	"start.rollback.word":   "retour arrière",
	"start.world.needsdb":   "world : démarre d'abord MySQL — sans base de données, worldserver ne démarre pas",
	"start.port.timeout":    "%s : le port %d ne s'est pas ouvert en %s",
	"start.db.unconfigured": "BD : vérification non configurée",
	"start.db.foreign":      "BD : le port %d est occupé par un processus étranger - il répond en TCP, mais pas comme MySQL (%v)",
	"start.db.prefix":       "BD",

	// --- worldlog.go -------------------------------------------------------
	"worldlog.partial": "le monde s'est arrêté sans sa boucle de sauvegarde : les pools de la base se sont fermés, " +
		"mais la commande `Server shutdown in N second(s)` n'a jamais tourné, donc les joueurs dans le monde " +
		"n'ont peut-être pas été sauvegardés. Les données peuvent avoir jusqu'à PlayerSaveInterval de retard " +
		"(15 minutes par défaut). La prochaine fois, arrête le monde avec Ctrl+C dans sa propre console — " +
		"c'est le seul chemin qui déclenche la sauvegarde.",
	"worldlog.unconfirmed": "impossible de confirmer l'ordre d'arrêt du monde via le log ; vérifie logs\\Server_*.log",

	// --- dumps.go ----------------------------------------------------------
	"dump.cancelled":       "la création de la copie a été annulée",
	"dump.create.fail":     "impossible de créer %s",
	"dump.write.fail":      "impossible d'écrire %s",
	"dump.fail.msg":        "le dump de %s a échoué : %s",
	"dump.fail":            "le dump de %s a échoué",
	"dump.read.fail":       "impossible de lire %s",
	"dump.phys.empty":      "%s est vide — aucune archive n'a été créée",
	"dump.norepack":        "repack introuvable : impossible de savoir où sont les bases et le dossier backups",
	"dump.nomysqldump":     "mysqldump introuvable : %s",
	"dump.nopassconf":      "%s introuvable — sans lui, le mot de passe de la base est inconnu",
	"dump.backupsdir.fail": "le dossier backups n'est pas accessible",
	"dump.data.missing":    "mysql\\data introuvable dans le repack",
	"dump.mysql.required":  "MySQL ne répond pas sur le port 3306 — les dumps se font sur un MySQL en marche",
	"dump.mysql.off":       "MySQL tourne : l'instantané ne se prend que base arrêtée, sinon la copie serait incohérente",
	"dump.world.off":       "worldserver tourne : arrête d'abord le serveur, puis la base",
	"dump.names.spent":     "%s a déjà ses cent noms pris pour %s",
	"dump.tail.read.fail":  "impossible de lire le dump %s",
	"dump.empty":           "le dump %s est vide",
	"dump.tail.end.fail":   "impossible de lire la fin du dump %s",
	"dump.truncated":       "le dump %s est tronqué : la dernière ligne n'est pas %q — le fichier est incomplet et ne doit pas être restauré",

	// --- restore.go --------------------------------------------------------
	"restore.norepack":    "repack introuvable : impossible de savoir où décompresser la copie",
	"restore.nofile":      "le dossier %s n'a pas de fichier %s",
	"restore.empty":       "%s est vide — la copie ne vaut rien",
	"restore.step.stop":   "arrêt du serveur",
	"restore.unavailable": "la restauration n'est pas disponible : le serveur n'est pas sous le contrôle du panneau",
	"restore.notstopped":  "le serveur ne s'est pas arrêté, la copie n'a pas été touchée",
	"restore.step.safety": "création de la copie de sécurité",
	"restore.safety.fail": "la copie de sécurité a échoué, le retour arrière est annulé",
	"restore.step.unpack": "décompression de la copie",
	"restore.step.done":   "fait",
	"restore.noenv":       "la restauration n'est pas disponible : l'environnement n'est pas accessible",
	"restore.unpack.fail": "la décompression a échoué",

	// --- backups.go --------------------------------------------------------
	"backups.nodir":    "le dossier des copies est inconnu",
	"backups.nopoint":  "aucun dossier de copie indiqué",
	"backups.nofolder": "backups\\ n'a pas de dossier %s",
	"backups.isfile":   "%s est un fichier, pas un dossier de copie",
	"backups.badname":  "le nom de dossier %q n'est pas valide : un nom de dossier dans backups\\ est attendu",
	"backups.notpoint": "ne ressemble pas à un point de retour arrière",
	"backups.nodump":   "pas de dump : %s",

	// --- realmdb.go --------------------------------------------------------
	"rdb.nodb":     "pas de connexion à la base",
	"rdb.sizes":    "tailles des bases",
	"rdb.chars":    "la liste des personnages",
	"rdb.accounts": "la liste des comptes",

	// --- prune.go, dbping.go, logx.go ---------------------------------------
	"prune.norepack": "repack introuvable : impossible de savoir où sont les copies",
	"prune.rm.fail":  "impossible de supprimer %s",
	"dbping.nopass":  "le mot de passe MySQL est vide — vérifie temp\\mysql_ac.conf",
	"dbping.open":    "ouverture de la base (%s)",
	"logx.mkdir":     "création du dossier de logs %s",
	"logx.open":      "ouverture du log %s",

	// --- osops -------------------------------------------------------------
	"console.noconsole": "le processus n'a pas de console",
	"console.denied":    "accès refusé — le serveur tourne en administrateur et le panneau non (ou l'inverse) ; lance les deux avec les mêmes droits",
	"console.badhandle": "handle invalide — le processus est déjà terminé",
	"console.nowindow":  "le processus n'a pas de console (lancé avec CREATE_NO_WINDOW)",
	"console.code":      "code %d — la console n'est pas accessible",
	"console.conin":     "ouverture de CONIN$ pour le PID %d",
	"console.write":     "WriteConsoleInput pour le PID %d",
	"console.wrote":     "WriteConsoleInput pour le PID %d : %d événements écrits sur %d",
	"console.ctrlevent": "GenerateConsoleCtrlEvent pour le PID %d",
	"ops.stdin":         "stdin pour %s",
	"ops.launch":        "démarrage de %s",
	"ops.tasklist":      "analyse de la sortie de tasklist",
	"ops.pslist":        "prise de l'instantané de la liste des processus",
	"ops.taskkill":      "taskkill pour le PID %d",
	"ops.open":          "ouvrir %s",

	// --- repack ------------------------------------------------------------
	"layout.selfpath":  "le chemin vers moi",
	"layout.emptypath": "le chemin de recherche est vide",
	"layout.abs":       "chemin absolu pour %q",
	"layout.almost":    "ressemble à un repack, mais il manque : %v (dossier %s)",
	"layout.notfound":  "racine du repack introuvable : en remontant depuis %s, aucun de %v n'a été rencontré",
	"mycnf.nopass":     "%s : la clé password est vide",
	"mycnf.read":       "lecture de %s",
	"mycnf.noclient":   "%s : la section [client] n'a pas été trouvée",

	// --- settings.go ---------------------------------------------------------
	"settings.prefix":     "réglages",
	"settings.unreadable": "réglages : le fichier %s ne peut pas être lu",
	"settings.nowhere":    "réglages : on ne sait où sauvegarder",

	// --- app.go ---------------------------------------------------------------
	"app.backup.norepack":    "repack introuvable — le dossier backups est inconnu",
	"app.backup.unavailable": "la création de copies n'est pas disponible : repack introuvable",
	"app.backup.busy":        "une copie est déjà en cours — attends la fin",
	"app.restore.norepack":   "la restauration n'est pas disponible : repack introuvable",
	"app.ops.busy":           "une autre opération de copies est en cours — attends la fin",
	"app.safety.nomysqldump": "la copie de sécurité n'est pas disponible : pas de mysqldump",
	"app.err.kind":           "type de copie inconnu %q (logical ou physical attendu)",
	"app.err.service":        "service inconnu %q (mysql, auth ou world attendu)",
	"app.restart.stopfail":   "redémarrage de %s : l'arrêt a échoué",
	"app.folder.unknown":     "dossier inconnu %q (backups, logs, configs ou root attendu)",
	"app.folder.norepack":    "repack introuvable — on ne sait quel dossier ouvrir",
	"app.folder.noenv":       "rien à ouvrir : l'environnement n'est pas accessible",
	"app.err.lang":           "langue inconnue %q (une de : %s attendue)",

	// --- setup checklist (app.go) ----------------------------------------------
	"setup.repack.label":    "Repack présent",
	"setup.repack.nofolder": "le dossier du repack n'a pas été trouvé",
	"setup.repack.fix":      "mets wow-hub.exe dans le dossier du repack et relance",
	"setup.repack.missing":  "il manque : %s",
	"setup.repack.fix2":     "décompresse le repack en entier",
	"setup.db.label":        "La base répond",
	"setup.db.notread":      "les données ne sont pas encore lues",
	"setup.db.fix":          "démarre MySQL",
	"setup.db.detail":       "%d bases, %s, %d comptes",
	"setup.rollback.label":  "Une copie récente existe",
	"setup.rollback.none":   "il n'y a pas de copies",
	"setup.rollback.fix":    "appuie sur Créer sur l'écran Copies",
	"setup.rollback.detail": "la dernière a été faite il y a %s (%s)",
	"setup.rollback.stale":  "fais une copie neuve : la dernière a plus de %s",
	"setup.space.label":     "Il y a assez d'espace disque",
	"setup.space.unknown":   "impossible de connaître l'espace libre",
	"setup.space.fix":       "vérifie que le dossier backups existe et est accessible",
	"setup.space.detail":    "%s libres",
	"setup.space.lowfix":    "une copie prend environ 500 Mo — libère de l'espace",
	"setup.server.label":    "Le serveur tourne",
	"setup.server.detail":   "%d sur %d en marche",
	"setup.server.fix":      "appuie sur Démarrer le serveur",

	// --- durations, units (app.go) ----------------------------------------------
	"duration.ltmin":   "moins d'une minute",
	"dur.minute.one":   "minute",
	"dur.minute.few":   "minutes",
	"dur.minute.many":  "minutes",
	"dur.minute.other": "minutes",
	"dur.hour.one":     "heure",
	"dur.hour.few":     "heures",
	"dur.hour.many":    "heures",
	"dur.hour.other":   "heures",
	"dur.day.one":      "jour",
	"dur.day.few":      "jours",
	"dur.day.many":     "jours",
	"dur.day.other":    "jours",
	"unit.b":           "B",
	"unit.kb":          "Ko",
	"unit.mb":          "Mo",
	"unit.gb":          "Go",
	"unit.tb":          "To",

	// --- main.go ------------------------------------------------------------
	"tray.open":      "Ouvrir le panneau",
	"tray.quit":      "Quitter",
	"fatal.title":    "Wow Hub — erreur au démarrage",
	"fatal.frontend": "impossible d'ouvrir le frontend intégré",
	"fatal.window":   "la fenêtre ne s'est pas ouverte",
}

func init() { register(Fr, dictFr) }
