package i18n

// dictRu is the source of truth: these are the exact strings that were inline
// in the code before the panel learned languages, byte for byte, so tests that
// pin UI text under Set(Ru) read the same sentences as before.
var dictRu = map[string]string{
	// --- health.go ---------------------------------------------------------
	"health.mysql.process.label": "MySQL: процесс",
	"health.mysql.port.label":    "MySQL: порт",
	"health.auth.port.label":     "Authserver: порт",
	"health.world.process.label": "Worldserver: процесс",
	"health.repack.label":        "Репак на месте",
	"health.repack.noroot":       "корень репака не определён",
	"health.path.missing":        "%s (не определён)",
	"health.repack.absent":       "отсутствует: %s",
	"health.port.probefail":      "не удалось опросить: %v",
	"health.port.notrunning":     "не запущен",
	"health.process.count":       "процессов: %d (",
	"health.process.portowner":   ", порт держит PID %d",
	"health.port.listening":      "порт %d слушает",
	"health.port.noanswer":       "порт %d не отвечает",
	"health.ping.label":          "MySQL: отвечает",
	"health.check.unconfigured":  "проверка не настроена",
	"health.ping.foreign": "порт %d занят посторонним процессом — он отвечает на TCP, " +
		"но не отвечает как MySQL: %v",
	"health.ping.ms": "ответ за %d мс",

	// --- supervisor.go -----------------------------------------------------
	"err.busy":           "операция уже выполняется — дождитесь окончания текущей",
	"process.ownerless":  "нет владельца порта, взят наименьший PID",
	"process.owner":      "взят владелец порта",
	"process.pids":       "%d процесса: %s; %s %d",
	"process.exited":     "процесс завершился сам",
	"process.exited.err": "процесс завершился сам: %v",

	// --- stop.go -----------------------------------------------------------
	"stop.denied":             "%s: остановка отменена — сначала остановите %s, иначе %s останется работать с мёртвой базой",
	"stop.unknown":            "%s: неизвестный сервис",
	"stop.kill.fail":          "%s (PID %d): не удалось убить процесс",
	"stop.kill.alive":         "%s (PID %d): процесс жив даже после kill",
	"stop.confirm.declined":   "остановка отменена пользователем: процесс не тронут",
	"stop.confirm.unanswered": "диалог подтверждения остался без ответа: процесс не тронут",
	"stop.mysql.noadmin":      "mysql: не найден mysqladmin.exe в раскладке репака",
	"stop.mysql.adminfail":    "mysql: mysqladmin shutdown не удался",
	"stop.mysql.alive":        "mysql: процесс %d жив после shutdown (порт %d закрылся, но mysqld не ушёл)",

	// --- start.go ----------------------------------------------------------
	"start.probe.fail":      "%s: не удалось проверить, не запущен ли он уже",
	"start.rollback.word":   "откат",
	"start.world.needsdb":   "world: сначала запустите MySQL — без базы worldserver не стартует",
	"start.port.timeout":    "%s: порт %d не открылся за %s",
	"start.db.unconfigured": "БД: проверка не настроена",
	"start.db.foreign": "БД: порт %d занят посторонним процессом - он отвечает на TCP, " +
		"но не отвечает как MySQL (%v)",
	"start.db.prefix": "БД",

	// --- worldlog.go -------------------------------------------------------
	"worldlog.partial": "мир остановлен без цикла сохранения: пулы БД закрылись, но команда " +
		"`Server shutdown in N second(s)` не выполнялась, поэтому игроки в игре могли не сохраниться. " +
		"Данные могут отставать до PlayerSaveInterval (по умолчанию 15 минут). " +
		"В следующий раз остановите мир через Ctrl+C в его окне — только он запускает сохранение.",
	"worldlog.unconfirmed": "не удалось подтвердить порядок остановки мира по логу; проверьте logs\\Server_*.log",

	// --- dumps.go ----------------------------------------------------------
	"dump.cancelled":       "создание копии отменено",
	"dump.create.fail":     "не удалось создать %s",
	"dump.write.fail":      "не удалось записать %s",
	"dump.fail.msg":        "дамп %s не удался: %s",
	"dump.fail":            "дамп %s не удался",
	"dump.read.fail":       "не удалось прочитать %s",
	"dump.phys.empty":      "%s пуст — архив не создан",
	"dump.norepack":        "репак не найден: неизвестно, где лежат базы и папка backups",
	"dump.nomysqldump":     "не найден mysqldump: %s",
	"dump.nopassconf":      "не найден %s — без него неизвестен пароль к базе",
	"dump.backupsdir.fail": "папка backups недоступна",
	"dump.data.missing":    "mysql\\data не найдена в репаке",
	"dump.mysql.required":  "MySQL не отвечает на порту 3306 — дампы снимаются на работающем MySQL",
	"dump.mysql.off": "MySQL запущен: слепок снимается только с выключенной базы, " +
		"иначе копия окажется несогласованной",
	"dump.world.off":      "worldserver запущен: сначала остановите сервер, потом базу",
	"dump.names.spent":    "в %s уже сто занятых имён для %s",
	"dump.tail.read.fail": "не удалось прочитать дамп %s",
	"dump.empty":          "дамп %s пуст",
	"dump.tail.end.fail":  "не удалось прочитать конец дампа %s",
	"dump.truncated":      "дамп %s оборван: последняя строка не %q — файл неполный, восстанавливать его нельзя",

	// --- restore.go --------------------------------------------------------
	"restore.norepack":    "репак не найден: неизвестно, куда распаковывать копию",
	"restore.nofile":      "в папке %s нет файла %s",
	"restore.empty":       "%s пуст — копия негодная",
	"restore.step.stop":   "останавливаю сервер",
	"restore.unavailable": "восстановление недоступно: сервер не под контролем панели",
	"restore.notstopped":  "сервер не остановился, копия не тронута",
	"restore.step.safety": "делаю страховочную копию",
	"restore.safety.fail": "страховочная копия не удалась, откат отменён",
	"restore.step.unpack": "распаковываю копию",
	"restore.step.done":   "готово",
	"restore.noenv":       "восстановление недоступно: среда недоступна",
	"restore.unpack.fail": "распаковка не удалась",

	// --- backups.go --------------------------------------------------------
	"backups.nodir":    "папка с копиями неизвестна",
	"backups.nopoint":  "не указана папка с копией",
	"backups.nofolder": "в backups\\ нет папки %s",
	"backups.isfile":   "%s — это файл, а не папка с копией",
	"backups.badname":  "имя папки %q недопустимо: ожидается имя папки внутри backups\\",
	"backups.notpoint": "не похоже на точку отката",
	"backups.nodump":   "нет дампа: %s",

	// --- realmdb.go --------------------------------------------------------
	"rdb.nodb":     "нет подключения к базе",
	"rdb.sizes":    "размеры баз",
	"rdb.chars":    "список персонажей",
	"rdb.accounts": "список аккаунтов",

	// --- prune.go, dbping.go, logx.go ---------------------------------------
	"prune.norepack": "репак не найден: неизвестно, где лежат копии",
	"prune.rm.fail":  "не удалось удалить %s",
	"dbping.nopass":  "пароль MySQL пуст — проверь temp\\mysql_ac.conf",
	"dbping.open":    "открытие БД (%s)",
	"logx.mkdir":     "создание каталога логов %s",
	"logx.open":      "открытие лога %s",

	// --- osops -------------------------------------------------------------
	"console.noconsole": "у процесса нет консоли",
	"console.denied": "доступ запрещён — сервер запущен от администратора, " +
		"а панель нет (или наоборот); запустите оба с одинаковыми правами",
	"console.badhandle": "недействительный дескриптор — процесс уже завершился",
	"console.nowindow":  "у процесса нет консоли (запущен с CREATE_NO_WINDOW)",
	"console.code":      "код %d — консоль недоступна",
	"console.conin":     "открытие CONIN$ для PID %d",
	"console.write":     "WriteConsoleInput для PID %d",
	"console.wrote":     "WriteConsoleInput для PID %d: записано %d из %d событий",
	"console.ctrlevent": "GenerateConsoleCtrlEvent для PID %d",
	"ops.stdin":         "stdin для %s",
	"ops.launch":        "запуск %s",
	"ops.tasklist":      "разбор вывода tasklist",
	"ops.pslist":        "снимок списка процессов",
	"ops.taskkill":      "taskkill для PID %d",
	"ops.open":          "открыть %s",

	// --- repack ------------------------------------------------------------
	"layout.selfpath":  "путь к себе",
	"layout.emptypath": "путь поиска пуст",
	"layout.abs":       "абсолютный путь для %q",
	"layout.almost":    "похоже на репак, но не хватает: %v (каталог %s)",
	"layout.notfound":  "корень репака не найден: поднимаясь от %s не встретилось ни одного из %v",
	"mycnf.nopass":     "%s: ключ password пуст",
	"mycnf.read":       "чтение %s",
	"mycnf.noclient":   "%s: секция [client] не найдена",

	// --- settings.go ---------------------------------------------------------
	"settings.prefix":     "настройки",
	"settings.unreadable": "настройки: файл %s не читается",
	"settings.nowhere":    "настройки: неизвестно, куда сохранять",

	// --- app.go ---------------------------------------------------------------
	"app.backup.norepack":    "репак не найден — папка backups неизвестна",
	"app.backup.unavailable": "создание копий недоступно: репак не найден",
	"app.backup.busy":        "копия уже создаётся — дождитесь окончания",
	"app.restore.norepack":   "восстановление недоступно: репак не найден",
	"app.ops.busy":           "сейчас идёт другая операция с копиями — дождитесь её окончания",
	"app.safety.nomysqldump": "страховочная копия недоступна: нет mysqldump",
	"app.err.kind":           "неизвестный вид копии %q (ожидается logical или physical)",
	"app.err.service":        "неизвестный сервис %q (ожидается mysql, auth или world)",
	"app.restart.stopfail":   "перезапуск %s: остановка не удалась",
	"app.folder.unknown":     "неизвестная папка %q (ожидается backups, logs, configs или root)",
	"app.folder.norepack":    "репак не найден — неизвестно, какую папку открывать",
	"app.folder.noenv":       "нечего открывать: среда недоступна",
	"app.err.lang":           "неизвестный язык %q (ожидается один из: %s)",

	// --- setup checklist (app.go) ----------------------------------------------
	"setup.repack.label":    "Репак на месте",
	"setup.repack.nofolder": "папка репака не найдена",
	"setup.repack.fix":      "положите wow-hub.exe в папку репака и запустите снова",
	"setup.repack.missing":  "не хватает: %s",
	"setup.repack.fix2":     "распакуйте репак полностью",
	"setup.db.label":        "База отвечает",
	"setup.db.notread":      "данные ещё не прочитаны",
	"setup.db.fix":          "запустите MySQL",
	"setup.db.detail":       "%d баз, %s, %d аккаунтов",
	"setup.rollback.label":  "Есть свежая копия",
	"setup.rollback.none":   "копий нет",
	"setup.rollback.fix":    "нажмите «Создать» на экране «Резервные копии»",
	"setup.rollback.detail": "последняя — %s назад (%s)",
	"setup.rollback.stale":  "сделайте новую копию: последняя старше %s",
	"setup.space.label":     "Хватает места на диске",
	"setup.space.unknown":   "не удалось узнать свободное место",
	"setup.space.fix":       "проверьте, что папка backups существует и доступна",
	"setup.space.detail":    "свободно %s",
	"setup.space.lowfix":    "копия занимает около 500 МБ — освободите место",
	"setup.server.label":    "Сервер запущен",
	"setup.server.detail":   "запущено %d из %d",
	"setup.server.fix":      "нажмите «Запустить сервер»",

	// --- durations, units (app.go) ----------------------------------------------
	"duration.ltmin":   "меньше минуты",
	"dur.minute.one":   "минуту",
	"dur.minute.few":   "минуты",
	"dur.minute.many":  "минут",
	"dur.minute.other": "минут",
	"dur.hour.one":     "час",
	"dur.hour.few":     "часа",
	"dur.hour.many":    "часов",
	"dur.hour.other":   "часов",
	"dur.day.one":      "день",
	"dur.day.few":      "дня",
	"dur.day.many":     "дней",
	"dur.day.other":    "дней",
	"unit.b":           "Б",
	"unit.kb":          "КБ",
	"unit.mb":          "МБ",
	"unit.gb":          "ГБ",
	"unit.tb":          "ТБ",

	// --- main.go ------------------------------------------------------------
	"tray.open":      "Открыть панель",
	"tray.quit":      "Выход",
	"fatal.title":    "Wow Hub — ошибка запуска",
	"fatal.frontend": "не удалось открыть встроенный фронтенд",
	"fatal.window":   "окно не открылось",
}

func init() { register(Ru, dictRu) }
