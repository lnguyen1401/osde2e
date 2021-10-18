package addons

import (
	"fmt"
	"log"
	"strings"

	"github.com/onsi/ginkgo"
	viper "github.com/openshift/osde2e/pkg/common/concurrentviper"

	"github.com/openshift/osde2e/pkg/common/alert"
	"github.com/openshift/osde2e/pkg/common/config"
	"github.com/openshift/osde2e/pkg/common/helper"
	"github.com/openshift/osde2e/pkg/common/prow"
)

var _ = ginkgo.Describe("[Suite: addons] Addon Test Harness", func() {
	defer ginkgo.GinkgoRecover()
	h := helper.New()

	addonTimeoutInSeconds := float64(viper.GetFloat64(config.Addons.PollingTimeout))
	log.Printf("addon timeout is %v", addonTimeoutInSeconds)
	ginkgo.It("should run until completion", func() {
		h.SetServiceAccount(viper.GetString(config.Addons.TestUser))
		harnesses := strings.Split(viper.GetString(config.Addons.TestHarnesses), ",")
		failed := h.RunAddonTests("addon-tests", int(addonTimeoutInSeconds), harnesses, []string{})
		if len(failed) > 0 {
			message := fmt.Sprintf("Addon tests failed: %v", failed)
			if url, ok := prow.JobURL(); ok {
				message += "\n" + url
			}
			if err := alert.SendSlackMessage(viper.GetString(config.Addons.SlackChannel), message); err != nil {
				log.Printf("Failed sending slack alert for addon failure: %v", err)
			}
		}
	}, addonTimeoutInSeconds+30)
})
