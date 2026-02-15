package strategies

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/strategies/httphc"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/strategies/mockhc"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/strategies/tcpconnhc"
)

func NewStrategy(name healthcheck.StrategyName, target healthcheck.TargetAddr, checkCfg []byte) (healthcheck.Strategy, error) {
	var (
		settingsVar any
		createFunc  func(any) (healthcheck.Strategy, error)
	)
	switch name {
	case healthcheck.HTTPStrategy:
		settingsVar = &httphc.HTTPStrategySettings{}
		createFunc = func(settings any) (healthcheck.Strategy, error) {
			return httphc.NewHTTPStrategy(settings.(*httphc.HTTPStrategySettings), target)
		}
	case healthcheck.TCPStrategy:
		settingsVar = &tcpconnhc.TcpConnStrategy{}
		createFunc = func(settings any) (healthcheck.Strategy, error) {
			return tcpconnhc.NewTcpConnStrategy(settings.(*tcpconnhc.TcpHealthCheckSettings), target)
		}
	case healthcheck.MockStrategy:
		settingsVar = &mockhc.MockHCSettings{}
		createFunc = func(settings any) (healthcheck.Strategy, error) {
			return mockhc.NewMockHC(settings.(*mockhc.MockHCSettings)), nil
		}
	default:
		settingsVar = new(string)
		createFunc = func(a any) (healthcheck.Strategy, error) {
			return mockhc.NewMockHC(&mockhc.MockHCSettings{
				Name:     "defaul mock healthcheck",
				Duration: 75 * time.Millisecond,
			}), nil
		}
	}

	err := json.Unmarshal(checkCfg, settingsVar)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal cfg for strategy: %s: %w", name, err)
	}
	return createFunc(settingsVar)
}
