package main

// AppService is what the frontend calls.
//
// Nineteen one-line delegates and nothing else. The logic lives in App, which
// does not import Wails and would keep working if the panel were served over
// HTTP instead. This type exists because Wails binds services, not structs, and
// App is the one struct we already had.
import (
	"wow-hub/internal/health"
	"wow-hub/internal/realmdb"
	"wow-hub/internal/supervisor"
)

type AppService struct{ app *App }

func newService(a *App) *AppService { return &AppService{app: a} }

func (s *AppService) Status() supervisor.Snapshot             { return s.app.Status() }
func (s *AppService) Health() []health.Check                  { return s.app.Health() }
func (s *AppService) Data() DataSnapshot                      { return s.app.Data() }
func (s *AppService) RefreshData()                            { s.app.RefreshData() }
func (s *AppService) Setup() []SetupItem                      { return s.app.Setup() }
func (s *AppService) BackupStatus() BackupStatus              { return s.app.BackupStatus() }
func (s *AppService) CancelBackup()                           { s.app.CancelBackup() }
func (s *AppService) RespondConfirm(ok bool)                  { s.app.RespondConfirm(ok) }
func (s *AppService) Characters(limit int) realmdb.Characters { return s.app.Characters(limit) }
func (s *AppService) Accounts(limit int) realmdb.Accounts     { return s.app.Accounts(limit) }

func (s *AppService) CreateBackup(kind string) error    { return s.app.CreateBackup(kind) }
func (s *AppService) RestoreSnapshot(name string) error { return s.app.RestoreSnapshot(name) }
func (s *AppService) DeleteBackup(name string) error    { return s.app.DeleteBackup(name) }
func (s *AppService) OpenFolder(which string) error     { return s.app.OpenFolder(which) }

func (s *AppService) Schedule() ScheduleState { return s.app.Schedule() }
func (s *AppService) SetSchedule(enabled bool, everyDays int, autoRun bool) error {
	return s.app.SetSchedule(enabled, everyDays, autoRun)
}

func (s *AppService) Language() string              { return s.app.Language() }
func (s *AppService) SetLanguage(lang string) error { return s.app.SetLanguage(lang) }

func (s *AppService) StartAll() error { return s.app.StartAll() }
func (s *AppService) StopAll() error  { return s.app.StopAll() }

func (s *AppService) Start(service string) error   { return s.app.Start(service) }
func (s *AppService) Stop(service string) error    { return s.app.Stop(service) }
func (s *AppService) Restart(service string) error { return s.app.Restart(service) }
