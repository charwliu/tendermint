package consensus

import (
	"github.com/tendermint/tendermint/Godeps/_workspace/src/github.com/tendermint/go-logger"
)

var log = logger.New("module", "consensus")

/*
func init() {
	log.SetHandler(
		logger.LvlFilterHandler(
			logger.LvlDebug,
			logger.BypassHandler(),
		),
	)
}
*/
