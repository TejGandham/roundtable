package roundtable

type Request struct {
	Prompt          string
	Files           []string
	Timeout         int
	Role            string
	Model           string
	Resume          string
	RolesDir        string
	ProjectRolesDir string
}

const DefaultTimeout = 900
