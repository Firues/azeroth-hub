package i18n

// dictEs is Spanish (neutral Latin American / Castilian mix common in games:
// tú addressing, no vosotros forms needed here).
var dictEs = map[string]string{
	// --- health.go ---------------------------------------------------------
	"health.mysql.process.label": "MySQL: proceso",
	"health.mysql.port.label":    "MySQL: puerto",
	"health.auth.port.label":     "Authserver: puerto",
	"health.world.process.label": "Worldserver: proceso",
	"health.repack.label":        "Repack presente",
	"health.repack.noroot":       "raíz del repack no encontrada",
	"health.path.missing":        "%s (no encontrado)",
	"health.repack.absent":       "falta: %s",
	"health.port.probefail":      "no se pudo sondear: %v",
	"health.port.notrunning":     "no está en ejecución",
	"health.process.count":       "procesos: %d (",
	"health.process.portowner":   ", puerto ocupado por el PID %d",
	"health.port.listening":      "el puerto %d está a la escucha",
	"health.port.noanswer":       "el puerto %d no responde",
	"health.ping.label":          "MySQL: respondiendo",
	"health.check.unconfigured":  "comprobación sin configurar",
	"health.ping.foreign":        "el puerto %d está ocupado por un proceso ajeno — responde a TCP, pero no como MySQL: %v",
	"health.ping.ms":             "respondió en %d ms",

	// --- supervisor.go -----------------------------------------------------
	"err.busy":           "ya se está ejecutando una operación — espera a que termine la actual",
	"process.ownerless":  "sin dueño del puerto, se tomó el PID más bajo",
	"process.owner":      "se tomó el dueño del puerto",
	"process.pids":       "%d procesos: %s; %s %d",
	"process.exited":     "el proceso terminó por sí solo",
	"process.exited.err": "el proceso terminó por sí solo: %v",

	// --- stop.go -----------------------------------------------------------
	"stop.denied":             "%s: detención cancelada — detén primero %s, o %s seguiría funcionando sobre una base de datos muerta",
	"stop.unknown":            "%s: servicio desconocido",
	"stop.kill.fail":          "%s (PID %d): no se pudo matar el proceso",
	"stop.kill.alive":         "%s (PID %d): el proceso sigue vivo incluso tras kill",
	"stop.confirm.declined":   "detención cancelada por el usuario: el proceso no fue tocado",
	"stop.confirm.unanswered": "el diálogo de confirmación nunca fue respondido: el proceso no fue tocado",
	"stop.mysql.noadmin":      "mysql: no se encontró mysqladmin.exe en la estructura del repack",
	"stop.mysql.adminfail":    "mysql: falló mysqladmin shutdown",
	"stop.mysql.alive":        "mysql: el proceso %d sigue vivo tras el shutdown (el puerto %d se cerró, pero mysqld no se fue)",

	// --- start.go ----------------------------------------------------------
	"start.probe.fail":      "%s: no se pudo comprobar si ya está en ejecución",
	"start.rollback.word":   "reversión",
	"start.world.needsdb":   "world: inicia MySQL primero — sin base de datos worldserver no arranca",
	"start.port.timeout":    "%s: el puerto %d no se abrió en %s",
	"start.db.unconfigured": "BD: comprobación sin configurar",
	"start.db.foreign":      "BD: el puerto %d está ocupado por un proceso ajeno - responde a TCP, pero no como MySQL (%v)",
	"start.db.prefix":       "BD",

	// --- worldlog.go -------------------------------------------------------
	"worldlog.partial": "el mundo se detuvo sin su ciclo de guardado: los pools de la base de datos se cerraron, " +
		"pero el comando `Server shutdown in N second(s)` nunca se ejecutó, así que los jugadores dentro del mundo " +
		"podrían no haberse guardado. Los datos pueden quedar rezagados hasta PlayerSaveInterval (15 minutos por defecto). " +
		"La próxima vez, detén el mundo con Ctrl+C en su propia consola — solo así se dispara el guardado.",
	"worldlog.unconfirmed": "no se pudo confirmar el orden de detención del mundo en el log; revisa logs\\Server_*.log",

	// --- dumps.go ----------------------------------------------------------
	"dump.cancelled":       "la creación de la copia fue cancelada",
	"dump.create.fail":     "no se pudo crear %s",
	"dump.write.fail":      "no se pudo escribir %s",
	"dump.fail.msg":        "el volcado de %s falló: %s",
	"dump.fail":            "el volcado de %s falló",
	"dump.read.fail":       "no se pudo leer %s",
	"dump.phys.empty":      "%s está vacío — no se creó ningún archivo",
	"dump.norepack":        "repack no encontrado: no se sabe dónde están las bases de datos ni la carpeta backups",
	"dump.nomysqldump":     "mysqldump no encontrado: %s",
	"dump.nopassconf":      "%s no encontrado — sin él se desconoce la contraseña de la base",
	"dump.backupsdir.fail": "la carpeta backups no es accesible",
	"dump.data.missing":    "no se encontró mysql\\data en el repack",
	"dump.mysql.required":  "MySQL no responde en el puerto 3306 — los volcados se toman de un MySQL en marcha",
	"dump.mysql.off":       "MySQL está en marcha: la instantánea solo se toma con la base apagada, de lo contrario la copia sería inconsistente",
	"dump.world.off":       "worldserver está en marcha: detén primero el servidor y luego la base de datos",
	"dump.names.spent":     "%s ya tiene los cien nombres ocupados para %s",
	"dump.tail.read.fail":  "no se pudo leer el volcado %s",
	"dump.empty":           "el volcado %s está vacío",
	"dump.tail.end.fail":   "no se pudo leer el final del volcado %s",
	"dump.truncated":       "el volcado %s está cortado: la última línea no es %q — el archivo está incompleto y no debe restaurarse",

	// --- restore.go --------------------------------------------------------
	"restore.norepack":    "repack no encontrado: no se sabe dónde desempaquetar la copia",
	"restore.nofile":      "la carpeta %s no tiene el archivo %s",
	"restore.empty":       "%s está vacío — la copia no sirve",
	"restore.step.stop":   "deteniendo el servidor",
	"restore.unavailable": "la restauración no está disponible: el servidor no está bajo el control del panel",
	"restore.notstopped":  "el servidor no se detuvo, la copia no fue tocada",
	"restore.step.safety": "haciendo la copia de seguridad",
	"restore.safety.fail": "la copia de seguridad falló, la reversión fue cancelada",
	"restore.step.unpack": "desempaquetando la copia",
	"restore.step.done":   "listo",
	"restore.noenv":       "la restauración no está disponible: el entorno no es accesible",
	"restore.unpack.fail": "la descompresión falló",

	// --- backups.go --------------------------------------------------------
	"backups.nodir":    "la carpeta de copias es desconocida",
	"backups.nopoint":  "no se indicó la carpeta de la copia",
	"backups.nofolder": "backups\\ no tiene la carpeta %s",
	"backups.isfile":   "%s es un archivo, no una carpeta de copia",
	"backups.badname":  "el nombre de carpeta %q no es válido: se espera un nombre de carpeta dentro de backups\\",
	"backups.notpoint": "no parece un punto de reversión",
	"backups.nodump":   "sin volcado: %s",

	// --- realmdb.go --------------------------------------------------------
	"rdb.nodb":     "sin conexión a la base de datos",
	"rdb.sizes":    "tamaños de las bases",
	"rdb.chars":    "la lista de personajes",
	"rdb.accounts": "la lista de cuentas",

	// --- prune.go, dbping.go, logx.go ---------------------------------------
	"prune.norepack": "repack no encontrado: no se sabe dónde están las copias",
	"prune.rm.fail":  "no se pudo eliminar %s",
	"dbping.nopass":  "la contraseña de MySQL está vacía — revisa temp\\mysql_ac.conf",
	"dbping.open":    "abriendo la base de datos (%s)",
	"logx.mkdir":     "creando el directorio de logs %s",
	"logx.open":      "abriendo el log %s",

	// --- osops -------------------------------------------------------------
	"console.noconsole": "el proceso no tiene consola",
	"console.denied":    "acceso denegado — el servidor corre como administrador y el panel no (o al revés); ejecuta ambos con los mismos derechos",
	"console.badhandle": "identificador inválido — el proceso ya terminó",
	"console.nowindow":  "el proceso no tiene consola (iniciado con CREATE_NO_WINDOW)",
	"console.code":      "código %d — la consola no es accesible",
	"console.conin":     "abriendo CONIN$ para el PID %d",
	"console.write":     "WriteConsoleInput para el PID %d",
	"console.wrote":     "WriteConsoleInput para el PID %d: se escribieron %d de %d eventos",
	"console.ctrlevent": "GenerateConsoleCtrlEvent para el PID %d",
	"ops.stdin":         "stdin para %s",
	"ops.launch":        "iniciando %s",
	"ops.tasklist":      "analizando la salida de tasklist",
	"ops.pslist":        "tomando la instantánea de la lista de procesos",
	"ops.taskkill":      "taskkill para el PID %d",
	"ops.open":          "abrir %s",

	// --- repack ------------------------------------------------------------
	"layout.selfpath":  "la ruta hacia mí",
	"layout.emptypath": "la ruta de búsqueda está vacía",
	"layout.abs":       "ruta absoluta para %q",
	"layout.almost":    "parece un repack, pero le falta: %v (directorio %s)",
	"layout.notfound":  "raíz del repack no encontrada: subiendo desde %s no se encontró ninguno de %v",
	"mycnf.nopass":     "%s: la clave password está vacía",
	"mycnf.read":       "leyendo %s",
	"mycnf.noclient":   "%s: no se encontró la sección [client]",

	// --- settings.go ---------------------------------------------------------
	"settings.prefix":     "ajustes",
	"settings.unreadable": "ajustes: el archivo %s no se puede leer",
	"settings.nowhere":    "ajustes: no se sabe dónde guardar",

	// --- app.go ---------------------------------------------------------------
	"app.backup.norepack":    "repack no encontrado — la carpeta backups es desconocida",
	"app.backup.unavailable": "la creación de copias no está disponible: repack no encontrado",
	"app.backup.busy":        "ya se está haciendo una copia — espera a que termine",
	"app.restore.norepack":   "la restauración no está disponible: repack no encontrado",
	"app.ops.busy":           "hay otra operación de copias en marcha — espera a que termine",
	"app.safety.nomysqldump": "la copia de seguridad no está disponible: no hay mysqldump",
	"app.err.kind":           "tipo de copia desconocido %q (se espera logical o physical)",
	"app.err.service":        "servicio desconocido %q (se espera mysql, auth o world)",
	"app.restart.stopfail":   "reiniciando %s: la detención falló",
	"app.folder.unknown":     "carpeta desconocida %q (se espera backups, logs, configs o root)",
	"app.folder.norepack":    "repack no encontrado — no se sabe qué carpeta abrir",
	"app.folder.noenv":       "nada que abrir: el entorno no es accesible",
	"app.err.lang":           "idioma desconocido %q (se espera uno de: %s)",

	// --- setup checklist (app.go) ----------------------------------------------
	"setup.repack.label":    "Repack presente",
	"setup.repack.nofolder": "no se encontró la carpeta del repack",
	"setup.repack.fix":      "pon wow-hub.exe en la carpeta del repack y vuelve a iniciar",
	"setup.repack.missing":  "falta: %s",
	"setup.repack.fix2":     "desempaqueta el repack por completo",
	"setup.db.label":        "La base de datos responde",
	"setup.db.notread":      "los datos aún no se han leído",
	"setup.db.fix":          "inicia MySQL",
	"setup.db.detail":       "%d bases, %s, %d cuentas",
	"setup.rollback.label":  "Existe una copia reciente",
	"setup.rollback.none":   "no hay copias",
	"setup.rollback.fix":    "pulsa Crear en la pantalla Copias",
	"setup.rollback.detail": "la última se hizo hace %s (%s)",
	"setup.rollback.stale":  "haz una copia nueva: la última tiene más de %s",
	"setup.space.label":     "Hay espacio en disco suficiente",
	"setup.space.unknown":   "no se pudo saber el espacio libre",
	"setup.space.fix":       "comprueba que la carpeta backups exista y sea accesible",
	"setup.space.detail":    "%s libres",
	"setup.space.lowfix":    "una copia ocupa unos 500 MB — libera espacio",
	"setup.server.label":    "El servidor está en marcha",
	"setup.server.detail":   "%d de %d en marcha",
	"setup.server.fix":      "pulsa Iniciar el servidor",

	// --- durations, units (app.go) ----------------------------------------------
	"duration.ltmin":   "menos de un minuto",
	"dur.minute.one":   "minuto",
	"dur.minute.few":   "minutos",
	"dur.minute.many":  "minutos",
	"dur.minute.other": "minutos",
	"dur.hour.one":     "hora",
	"dur.hour.few":     "horas",
	"dur.hour.many":    "horas",
	"dur.hour.other":   "horas",
	"dur.day.one":      "día",
	"dur.day.few":      "días",
	"dur.day.many":     "días",
	"dur.day.other":    "días",
	"unit.b":           "B",
	"unit.kb":          "KB",
	"unit.mb":          "MB",
	"unit.gb":          "GB",
	"unit.tb":          "TB",

	// --- main.go ------------------------------------------------------------
	"tray.open":      "Abrir el panel",
	"tray.quit":      "Salir",
	"fatal.title":    "Wow Hub — error de inicio",
	"fatal.frontend": "no se pudo abrir el frontend integrado",
	"fatal.window":   "la ventana no se abrió",
}

func init() { register(Es, dictEs) }
