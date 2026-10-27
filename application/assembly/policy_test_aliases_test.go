package assembly

import applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"

type policyApplication = applicationpolicy.SwitchApplication
type policyApplicationDeps = applicationpolicy.SwitchDeps

func newPolicyApplication(deps policyApplicationDeps) *policyApplication {
	return applicationpolicy.NewSwitch(deps)
}
