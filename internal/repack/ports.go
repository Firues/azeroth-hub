package repack

// The ports this repack's three servers listen on.
//
// They live here, in the package that already owns every other fact about
// this install, because three packages need them - the supervisor (readiness
// waits and port ownership), health (the port checks) and dbping (the
// connection). They used to be declared separately in each, which meant a
// repack configured on different ports had to be edited in three places, and
// a missed one would report a healthy server as dead.
//
// Values verified against the repack's own configs:
// configs/worldserver.conf, configs/authserver.conf and MySQL.bat.
const (
	MySQLPort = 3306
	AuthPort  = 3724
	WorldPort = 8085
)
