package run

import (
	ssh "github.com/mmussett/extensions/SSH/connector/connection"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/support/connection"
)

// Input corresponds to activity.json inputs
type Input struct {
	Connection        connection.Manager `md:"SSH Connection,required"`
	Cmd               string             `md:"cmd,required"`
	Stdin             string             `md:"stdin"`
	TimeoutSeconds    int                `md:"timeoutSeconds"`
	FailOnNonZeroExit bool               `md:"failOnNonZeroExit"`
}

// Output corresponds to activity.json outputs
type Output struct {
	StdOut     string `md:"stdOut,required"`
	StdErr     string `md:"stdErr"`
	ExitCode   int    `md:"exitCode"`
	DurationMs int    `md:"durationMs"`
}

// ToMap converts Input struct to map
func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"SSH Connection":    i.Connection,
		"cmd":               i.Cmd,
		"stdin":             i.Stdin,
		"timeoutSeconds":    i.TimeoutSeconds,
		"failOnNonZeroExit": i.FailOnNonZeroExit,
	}
}

// FromMap converts a map to Input struct
func (i *Input) FromMap(values map[string]interface{}) error {
	// unmapped keeps the original behaviour: a non-zero exit fails the activity
	i.FailOnNonZeroExit = true

	var err error
	i.Connection, err = ssh.GetSharedConfiguration(values["SSH Connection"])
	if err != nil {
		return err
	}

	i.Cmd, err = coerce.ToString(values["cmd"])
	if err != nil {
		return err
	}

	if i.Stdin, err = coerce.ToString(values["stdin"]); err != nil {
		return err
	}

	if v := values["timeoutSeconds"]; v != nil && v != "" {
		if i.TimeoutSeconds, err = coerce.ToInt(v); err != nil {
			return err
		}
	}

	if v := values["failOnNonZeroExit"]; v != nil && v != "" {
		if i.FailOnNonZeroExit, err = coerce.ToBool(v); err != nil {
			return err
		}
	}
	return nil
}

// ToMap converts Output struct to map
func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"stdOut":     o.StdOut,
		"stdErr":     o.StdErr,
		"exitCode":   o.ExitCode,
		"durationMs": o.DurationMs,
	}
}

// FromMap converts a map to Output struct
func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	if o.StdOut, err = coerce.ToString(values["stdOut"]); err != nil {
		return err
	}
	if o.StdErr, err = coerce.ToString(values["stdErr"]); err != nil {
		return err
	}
	if o.ExitCode, err = coerce.ToInt(values["exitCode"]); err != nil {
		return err
	}
	o.DurationMs, err = coerce.ToInt(values["durationMs"])
	return err
}
