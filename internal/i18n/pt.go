package i18n

// dictPt is Brazilian Portuguese, the dominant variant of the WoW player
// base that reads Portuguese.
var dictPt = map[string]string{
	// --- health.go ---------------------------------------------------------
	"health.mysql.process.label": "MySQL: processo",
	"health.mysql.port.label":    "MySQL: porta",
	"health.auth.port.label":     "Authserver: porta",
	"health.world.process.label": "Worldserver: processo",
	"health.repack.label":        "Repack no lugar",
	"health.repack.noroot":       "raiz do repack não encontrada",
	"health.path.missing":        "%s (não encontrado)",
	"health.repack.absent":       "faltando: %s",
	"health.port.probefail":      "não foi possível sondar: %v",
	"health.port.notrunning":     "não está em execução",
	"health.process.count":       "processos: %d (",
	"health.process.portowner":   ", porta ocupada pelo PID %d",
	"health.port.listening":      "a porta %d está em escuta",
	"health.port.noanswer":       "a porta %d não responde",
	"health.ping.label":          "MySQL: respondendo",
	"health.check.unconfigured":  "verificação não configurada",
	"health.ping.foreign":        "a porta %d está ocupada por um processo estranho — responde a TCP, mas não como MySQL: %v",
	"health.ping.ms":             "respondeu em %d ms",

	// --- supervisor.go -----------------------------------------------------
	"err.busy":           "uma operação já está em execução — aguarde o término da atual",
	"process.ownerless":  "sem dono da porta, tomado o menor PID",
	"process.owner":      "tomado o dono da porta",
	"process.pids":       "%d processos: %s; %s %d",
	"process.exited":     "o processo saiu por conta própria",
	"process.exited.err": "o processo saiu por conta própria: %v",

	// --- stop.go -----------------------------------------------------------
	"stop.denied":             "%s: parada cancelada — pare primeiro %s, senão %s continuaria rodando contra um banco morto",
	"stop.unknown":            "%s: serviço desconhecido",
	"stop.kill.fail":          "%s (PID %d): não foi possível matar o processo",
	"stop.kill.alive":         "%s (PID %d): o processo segue vivo mesmo após o kill",
	"stop.confirm.declined":   "parada cancelada pelo usuário: o processo não foi tocado",
	"stop.confirm.unanswered": "o diálogo de confirmação ficou sem resposta: o processo não foi tocado",
	"stop.mysql.noadmin":      "mysql: mysqladmin.exe não encontrado na estrutura do repack",
	"stop.mysql.adminfail":    "mysql: mysqladmin shutdown falhou",
	"stop.mysql.alive":        "mysql: o processo %d segue vivo após o shutdown (a porta %d fechou, mas o mysqld não foi embora)",

	// --- start.go ----------------------------------------------------------
	"start.probe.fail":      "%s: não foi possível verificar se já está em execução",
	"start.rollback.word":   "reversão",
	"start.world.needsdb":   "world: inicie o MySQL primeiro — sem banco de dados o worldserver não sobe",
	"start.port.timeout":    "%s: a porta %d não abriu em %s",
	"start.db.unconfigured": "BD: verificação não configurada",
	"start.db.foreign":      "BD: a porta %d está ocupada por um processo estranho - responde a TCP, mas não como MySQL (%v)",
	"start.db.prefix":       "BD",

	// --- worldlog.go -------------------------------------------------------
	"worldlog.partial": "o mundo parou sem o ciclo de salvamento: os pools do banco fecharam, " +
		"mas o comando `Server shutdown in N second(s)` nunca rodou, então os jogadores dentro do mundo " +
		"podem não ter sido salvos. Os dados podem ficar até PlayerSaveInterval atrás (15 minutos por padrão). " +
		"Da próxima vez, pare o mundo com Ctrl+C na janela dele — só esse caminho dispara o salvamento.",
	"worldlog.unconfirmed": "não foi possível confirmar a ordem de parada do mundo pelo log; confira logs\\Server_*.log",

	// --- dumps.go ----------------------------------------------------------
	"dump.cancelled":         "a criação da cópia foi cancelada",
	"dump.create.fail":       "não foi possível criar %s",
	"dump.write.fail":        "não foi possível gravar %s",
	"dump.fail.msg":          "o dump de %s falhou: %s",
	"dump.fail":              "o dump de %s falhou",
	"dump.read.fail":         "não foi possível ler %s",
	"dump.phys.nodir":        "o script terminou bem, mas nenhuma pasta nova %s apareceu em %s",
	"dump.phys.noarchive":    "o script terminou bem, mas %s não apareceu",
	"dump.phys.empty":        "%s está vazio — nenhum arquivo foi criado",
	"dump.norepack":          "repack não encontrado: não se sabe onde ficam os bancos e a pasta backups",
	"dump.nomysqldump":       "mysqldump não encontrado: %s",
	"dump.nopassconf":        "%s não encontrado — sem ele a senha do banco é desconhecida",
	"dump.backupsdir.fail":   "a pasta backups não está acessível",
	"dump.mysql.required":    "MySQL não responde na porta 3306 — os dumps são tirados com o MySQL rodando",
	"dump.noscript":          "%s não encontrado em %s",
	"dump.mysql.off":         "MySQL está rodando: o snapshot só é tirado com o banco parado, senão a cópia fica inconsistente",
	"dump.world.off":         "worldserver está rodando: pare primeiro o servidor, depois o banco",
	"dump.names.spent":       "%s já tem os cem nomes ocupados para %s",
	"dump.tail.read.fail":    "não foi possível ler o dump %s",
	"dump.empty":             "o dump %s está vazio",
	"dump.tail.end.fail":     "não foi possível ler o final do dump %s",
	"dump.truncated":         "o dump %s está truncado: a última linha não é %q — o arquivo está incompleto e não deve ser restaurado",
	"dump.snapshot.fail.msg": "o snapshot não foi criado: %s",
	"dump.snapshot.fail":     "o snapshot não foi criado",

	// --- restore.go --------------------------------------------------------
	"restore.norepack":    "repack não encontrado: não se sabe onde desempacotar a cópia",
	"restore.nofile":      "a pasta %s não tem o arquivo %s",
	"restore.empty":       "%s está vazio — a cópia não presta",
	"restore.step.stop":   "parando o servidor",
	"restore.unavailable": "a restauração não está disponível: o servidor não está sob o controle do painel",
	"restore.notstopped":  "o servidor não parou, a cópia não foi tocada",
	"restore.step.safety": "fazendo a cópia de segurança",
	"restore.safety.fail": "a cópia de segurança falhou, a reversão foi cancelada",
	"restore.step.unpack": "desempacotando a cópia",
	"restore.step.done":   "pronto",
	"restore.noenv":       "a restauração não está disponível: o ambiente não está acessível",
	"restore.no7zip":      "7-Zip não encontrado: %s — é ele quem desempacota a cópia",
	"restore.unpack.fail": "a descompactação falhou",

	// --- backups.go --------------------------------------------------------
	"backups.nodir":    "a pasta de cópias é desconhecida",
	"backups.nopoint":  "a pasta da cópia não foi indicada",
	"backups.nofolder": "backups\\ não tem a pasta %s",
	"backups.isfile":   "%s é um arquivo, não uma pasta de cópia",
	"backups.badname":  "o nome de pasta %q não é válido: espera-se um nome de pasta dentro de backups\\",
	"backups.notpoint": "não parece um ponto de reversão",
	"backups.nodump":   "sem dump: %s",

	// --- realmdb.go --------------------------------------------------------
	"rdb.nodb":     "sem conexão ao banco",
	"rdb.sizes":    "tamanhos dos bancos",
	"rdb.chars":    "a lista de personagens",
	"rdb.accounts": "a lista de contas",

	// --- prune.go, dbping.go, logx.go ---------------------------------------
	"prune.norepack": "repack não encontrado: não se sabe onde ficam as cópias",
	"prune.rm.fail":  "não foi possível excluir %s",
	"dbping.nopass":  "a senha do MySQL está vazia — confira temp\\mysql_ac.conf",
	"dbping.open":    "abrindo o banco (%s)",
	"logx.mkdir":     "criando o diretório de logs %s",
	"logx.open":      "abrindo o log %s",

	// --- osops -------------------------------------------------------------
	"console.noconsole": "o processo não tem console",
	"console.denied":    "acesso negado — o servidor roda como administrador e o painel não (ou o contrário); rode os dois com os mesmos direitos",
	"console.badhandle": "handle inválido — o processo já saiu",
	"console.nowindow":  "o processo não tem console (iniciado com CREATE_NO_WINDOW)",
	"console.code":      "código %d — o console não está acessível",
	"console.conin":     "abrindo CONIN$ para o PID %d",
	"console.write":     "WriteConsoleInput para o PID %d",
	"console.wrote":     "WriteConsoleInput para o PID %d: escreveu %d de %d eventos",
	"console.ctrlevent": "GenerateConsoleCtrlEvent para o PID %d",
	"ops.stdin":         "stdin para %s",
	"ops.launch":        "iniciando %s",
	"ops.tasklist":      "analisando a saída do tasklist",
	"ops.pslist":        "tirando o snapshot da lista de processos",
	"ops.taskkill":      "taskkill para o PID %d",
	"ops.open":          "abrir %s",

	// --- repack ------------------------------------------------------------
	"layout.selfpath":  "o caminho até mim",
	"layout.emptypath": "o caminho de busca está vazio",
	"layout.abs":       "caminho absoluto para %q",
	"layout.almost":    "parece um repack, mas está faltando: %v (diretório %s)",
	"layout.notfound":  "raiz do repack não encontrada: subindo a partir de %s não se encontrou nenhum de %v",
	"mycnf.nopass":     "%s: a chave password está vazia",
	"mycnf.read":       "lendo %s",
	"mycnf.noclient":   "%s: a seção [client] não foi encontrada",

	// --- settings.go ---------------------------------------------------------
	"settings.prefix":     "configurações",
	"settings.unreadable": "configurações: o arquivo %s não pode ser lido",
	"settings.nowhere":    "configurações: não se sabe onde salvar",

	// --- app.go ---------------------------------------------------------------
	"app.backup.norepack":    "repack não encontrado — a pasta backups é desconhecida",
	"app.backup.unavailable": "a criação de cópias não está disponível: repack não encontrado",
	"app.backup.busy":        "uma cópia já está sendo feita — aguarde o término",
	"app.restore.norepack":   "a restauração não está disponível: repack não encontrado",
	"app.ops.busy":           "outra operação de cópias está em andamento — aguarde o término",
	"app.safety.nomysqldump": "a cópia de segurança não está disponível: sem mysqldump",
	"app.err.kind":           "tipo de cópia desconhecido %q (espera-se logical ou physical)",
	"app.err.service":        "serviço desconhecido %q (espera-se mysql, auth ou world)",
	"app.restart.stopfail":   "reiniciando %s: a parada falhou",
	"app.folder.unknown":     "pasta desconhecida %q (espera-se backups, logs, configs ou root)",
	"app.folder.norepack":    "repack não encontrado — não se sabe qual pasta abrir",
	"app.folder.noenv":       "nada para abrir: o ambiente não está acessível",
	"app.err.lang":           "idioma desconhecido %q (espera-se um de: %s)",

	// --- setup checklist (app.go) ----------------------------------------------
	"setup.repack.label":    "Repack no lugar",
	"setup.repack.nofolder": "a pasta do repack não foi encontrada",
	"setup.repack.fix":      "coloque o wow-hub.exe na pasta do repack e inicie de novo",
	"setup.repack.missing":  "faltando: %s",
	"setup.repack.fix2":     "desempacote o repack por completo",
	"setup.db.label":        "O banco responde",
	"setup.db.notread":      "os dados ainda não foram lidos",
	"setup.db.fix":          "inicie o MySQL",
	"setup.db.detail":       "%d bancos, %s, %d contas",
	"setup.rollback.label":  "Existe uma cópia recente",
	"setup.rollback.none":   "não há cópias",
	"setup.rollback.fix":    "aperte Criar na tela Cópias",
	"setup.rollback.detail": "a última foi feita há %s (%s)",
	"setup.rollback.stale":  "faça uma cópia nova: a última tem mais de %s",
	"setup.space.label":     "Há espaço em disco suficiente",
	"setup.space.unknown":   "não foi possível saber o espaço livre",
	"setup.space.fix":       "confira que a pasta backups existe e está acessível",
	"setup.space.detail":    "%s livres",
	"setup.space.lowfix":    "uma cópia ocupa cerca de 500 MB — libere espaço",
	"setup.server.label":    "O servidor está rodando",
	"setup.server.detail":   "%d de %d rodando",
	"setup.server.fix":      "aperte Iniciar o servidor",

	// --- durations, units (app.go) ----------------------------------------------
	"duration.ltmin":   "menos de um minuto",
	"dur.minute.one":   "minuto",
	"dur.minute.few":   "minutos",
	"dur.minute.many":  "minutos",
	"dur.minute.other": "minutos",
	"dur.hour.one":     "hora",
	"dur.hour.few":     "horas",
	"dur.hour.many":    "horas",
	"dur.hour.other":   "horas",
	"dur.day.one":      "dia",
	"dur.day.few":      "dias",
	"dur.day.many":     "dias",
	"dur.day.other":    "dias",
	"unit.b":           "B",
	"unit.kb":          "KB",
	"unit.mb":          "MB",
	"unit.gb":          "GB",
	"unit.tb":          "TB",

	// --- main.go ------------------------------------------------------------
	"tray.open":      "Abrir o painel",
	"tray.quit":      "Sair",
	"fatal.title":    "Wow Hub — erro de inicialização",
	"fatal.frontend": "não foi possível abrir o frontend embutido",
	"fatal.window":   "a janela não abriu",
}

func init() { register(Pt, dictPt) }
