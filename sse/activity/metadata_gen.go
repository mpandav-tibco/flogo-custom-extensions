package ssesend

import (
	"github.com/project-flogo/core/activity"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})
