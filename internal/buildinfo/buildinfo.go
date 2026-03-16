package buildinfo

import "fmt"

var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

type Info struct {
	Version   string
	Commit    string
	BuildTime string
}

func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
	}
}

func (i Info) String() string {
	return fmt.Sprintf("version=%s commit=%s build_time=%s", i.Version, i.Commit, i.BuildTime)
}
