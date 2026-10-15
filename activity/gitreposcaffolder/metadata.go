package gitreposcaffolder

import (
	"fmt"

	"github.com/project-flogo/core/data/coerce"
)

const (
	TargetModeSubfolder = "subfolder"
	TargetModeNewRepo   = "newRepo"

	WriteStrategyAuto = "auto"
	WriteStrategyAPI  = "api"
	WriteStrategyGit  = "git"

	defaultAPIBaseURL = "https://api.github.com"
)

type Input struct {
	AuthToken         string            `md:"authToken"`
	TemplateAuthToken string            `md:"templateAuthToken"`
	TemplateRepoURL   string            `md:"templateRepoURL"`
	TemplateRef       string            `md:"templateRef"`
	TemplatePath      string            `md:"templatePath"`
	Replacements      map[string]string `md:"replacements"`
	ReplacementList   []interface{}     `md:"replacementList"`
	IgnoreEmptyValues bool              `md:"ignoreEmptyValues"`
	ReplacementMode   string            `md:"replacementMode"`
	RenamePaths       bool              `md:"renamePaths"`
	ExcludePatterns   []string          `md:"excludePatterns"`
	BinaryExtensions  []string          `md:"binaryExtensions"`
	MaxTextBytes      int               `md:"maxTextBytes"`
	OutputPrefix      string            `md:"outputPrefix"`
	ExtraFiles        []interface{}     `md:"extraFiles"`
	TargetMode        string            `md:"targetMode"`
	TargetRepoURL     string            `md:"targetRepoURL"`
	TargetBranch      string            `md:"targetBranch"`
	TargetFolder      string            `md:"targetFolder"`
	CleanTargetFolder bool              `md:"cleanTargetFolder"`
	NewRepoPrivate    bool              `md:"newRepoPrivate"`
	NewRepoDesc       string            `md:"newRepoDescription"`
	FailIfRepoExists  bool              `md:"failIfRepoExists"`
	APIBaseURL        string            `md:"apiBaseURL"`
	CommitMessage     string            `md:"commitMessage"`
	AuthorName        string            `md:"authorName"`
	AuthorEmail       string            `md:"authorEmail"`
	DryRun            bool              `md:"dryRun"`
	PushRetries       int               `md:"pushRetries"`
	WriteStrategy     string            `md:"writeStrategy"`
	TimeoutSeconds    int               `md:"timeoutSeconds"`
}

// defaults applied when an input is not mapped
func newInput() *Input {
	return &Input{
		ReplacementMode:   ReplacementModeSequential,
		RenamePaths:       true,
		MaxTextBytes:      defaultMaxTextBytes,
		TargetMode:        TargetModeSubfolder,
		TargetBranch:      "main",
		CleanTargetFolder: true,
		NewRepoPrivate:    true,
		FailIfRepoExists:  true,
		APIBaseURL:        defaultAPIBaseURL,
		CommitMessage:     "Scaffold from template",
		AuthorName:        "Flogo Git Repo Scaffolder",
		AuthorEmail:       "git-repo-scaffolder@flogo.local",
		PushRetries:       3,
		WriteStrategy:     WriteStrategyAuto,
		TimeoutSeconds:    120,
	}
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"authToken":          i.AuthToken,
		"templateAuthToken":  i.TemplateAuthToken,
		"templateRepoURL":    i.TemplateRepoURL,
		"templateRef":        i.TemplateRef,
		"templatePath":       i.TemplatePath,
		"replacements":       i.Replacements,
		"replacementList":    i.ReplacementList,
		"ignoreEmptyValues":  i.IgnoreEmptyValues,
		"replacementMode":    i.ReplacementMode,
		"renamePaths":        i.RenamePaths,
		"excludePatterns":    i.ExcludePatterns,
		"binaryExtensions":   i.BinaryExtensions,
		"maxTextBytes":       i.MaxTextBytes,
		"outputPrefix":       i.OutputPrefix,
		"extraFiles":         i.ExtraFiles,
		"targetMode":         i.TargetMode,
		"targetRepoURL":      i.TargetRepoURL,
		"targetBranch":       i.TargetBranch,
		"targetFolder":       i.TargetFolder,
		"cleanTargetFolder":  i.CleanTargetFolder,
		"newRepoPrivate":     i.NewRepoPrivate,
		"newRepoDescription": i.NewRepoDesc,
		"failIfRepoExists":   i.FailIfRepoExists,
		"apiBaseURL":         i.APIBaseURL,
		"commitMessage":      i.CommitMessage,
		"authorName":         i.AuthorName,
		"authorEmail":        i.AuthorEmail,
		"dryRun":             i.DryRun,
		"pushRetries":        i.PushRetries,
		"writeStrategy":      i.WriteStrategy,
		"timeoutSeconds":     i.TimeoutSeconds,
	}
}

func (i *Input) FromMap(values map[string]interface{}) error {
	strs := map[string]*string{
		"authToken":          &i.AuthToken,
		"templateAuthToken":  &i.TemplateAuthToken,
		"templateRepoURL":    &i.TemplateRepoURL,
		"templateRef":        &i.TemplateRef,
		"templatePath":       &i.TemplatePath,
		"replacementMode":    &i.ReplacementMode,
		"targetMode":         &i.TargetMode,
		"targetRepoURL":      &i.TargetRepoURL,
		"targetBranch":       &i.TargetBranch,
		"targetFolder":       &i.TargetFolder,
		"newRepoDescription": &i.NewRepoDesc,
		"apiBaseURL":         &i.APIBaseURL,
		"writeStrategy":      &i.WriteStrategy,
		"outputPrefix":       &i.OutputPrefix,
		"commitMessage":      &i.CommitMessage,
		"authorName":         &i.AuthorName,
		"authorEmail":        &i.AuthorEmail,
	}
	for k, p := range strs {
		if v, ok := values[k]; ok && v != nil {
			s, err := coerce.ToString(v)
			if err != nil {
				return fmt.Errorf("input %s: %w", k, err)
			}
			if s != "" {
				*p = s
			}
		}
	}

	bools := map[string]*bool{
		"ignoreEmptyValues": &i.IgnoreEmptyValues,
		"renamePaths":       &i.RenamePaths,
		"cleanTargetFolder": &i.CleanTargetFolder,
		"newRepoPrivate":    &i.NewRepoPrivate,
		"failIfRepoExists":  &i.FailIfRepoExists,
		"dryRun":            &i.DryRun,
	}
	for k, p := range bools {
		if v, ok := values[k]; ok && v != nil && v != "" {
			b, err := coerce.ToBool(v)
			if err != nil {
				return fmt.Errorf("input %s: %w", k, err)
			}
			*p = b
		}
	}

	ints := map[string]*int{
		"maxTextBytes":   &i.MaxTextBytes,
		"pushRetries":    &i.PushRetries,
		"timeoutSeconds": &i.TimeoutSeconds,
	}
	for k, p := range ints {
		if v, ok := values[k]; ok && v != nil && v != "" {
			n, err := coerce.ToInt(v)
			if err != nil {
				return fmt.Errorf("input %s: %w", k, err)
			}
			if n > 0 {
				*p = n
			}
		}
	}

	if v, ok := values["replacements"]; ok && v != nil && v != "" {
		obj, err := coerce.ToObject(v)
		if err != nil {
			return fmt.Errorf("input replacements must be an object of token -> value: %w", err)
		}
		i.Replacements = make(map[string]string, len(obj))
		for k, raw := range obj {
			if raw == nil {
				continue
			}
			s, err := coerce.ToString(raw)
			if err != nil {
				return fmt.Errorf("replacement %q: %w", k, err)
			}
			i.Replacements[k] = s
		}
	}

	if v, ok := values["replacementList"]; ok && v != nil && v != "" {
		arr, err := coerce.ToArray(v)
		if err != nil {
			return fmt.Errorf("input replacementList must be an array of {token, value}: %w", err)
		}
		if i.Replacements == nil {
			i.Replacements = make(map[string]string, len(arr))
		}
		for n, item := range arr {
			obj, err := coerce.ToObject(item)
			if err != nil {
				return fmt.Errorf("replacementList[%d]: %w", n, err)
			}
			token, _ := coerce.ToString(obj["token"])
			if token == "" || obj["value"] == nil {
				continue
			}
			val, err := coerce.ToString(obj["value"])
			if err != nil {
				return fmt.Errorf("replacementList[%d] value: %w", n, err)
			}
			i.Replacements[token] = val
		}
	}

	var err error
	if v := values["extraFiles"]; v != nil && v != "" {
		if i.ExtraFiles, err = coerce.ToArray(v); err != nil {
			return fmt.Errorf("input extraFiles: %w", err)
		}
		if _, err = toExtraFiles(i.ExtraFiles); err != nil {
			return fmt.Errorf("input extraFiles: %w", err)
		}
	}
	if i.ExcludePatterns, err = toStrings(values["excludePatterns"]); err != nil {
		return fmt.Errorf("input excludePatterns: %w", err)
	}
	if i.BinaryExtensions, err = toStrings(values["binaryExtensions"]); err != nil {
		return fmt.Errorf("input binaryExtensions: %w", err)
	}
	return nil
}

// toExtraFiles reads [{path, content}] (content as text) into file entries.
func toExtraFiles(v interface{}) ([]FileEntry, error) {
	if v == nil || v == "" {
		return nil, nil
	}
	arr, err := coerce.ToArray(v)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0, len(arr))
	for n, item := range arr {
		obj, err := coerce.ToObject(item)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", n, err)
		}
		p, _ := coerce.ToString(obj["path"])
		if p == "" {
			continue
		}
		content, err := coerce.ToString(obj["content"])
		if err != nil {
			return nil, fmt.Errorf("[%d] content: %w", n, err)
		}
		out = append(out, FileEntry{Path: p, Data: []byte(content)})
	}
	return out, nil
}

func toStrings(v interface{}) ([]string, error) {
	if v == nil || v == "" {
		return nil, nil
	}
	arr, err := coerce.ToArray(v)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, err := coerce.ToString(item)
		if err != nil {
			return nil, err
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

type Output struct {
	RepoURL        string   `md:"repoURL"`
	FolderURL      string   `md:"folderURL"`
	Branch         string   `md:"branch"`
	CommitSHA      string   `md:"commitSHA"`
	TemplateCommit string   `md:"templateCommit"`
	Pushed         bool     `md:"pushed"`
	WriteMode      string   `md:"writeMode"`
	RepoCreated    bool     `md:"repoCreated"`
	FilesTotal     int      `md:"filesTotal"`
	FilesModified  int      `md:"filesModified"`
	PathsRenamed   int      `md:"pathsRenamed"`
	FilesExcluded  int      `md:"filesExcluded"`
	Files          []string `md:"files"`
	DurationMs     int      `md:"durationMs"`
}

func (o *Output) ToMap() map[string]interface{} {
	files := make([]interface{}, len(o.Files))
	for i, f := range o.Files {
		files[i] = f
	}
	return map[string]interface{}{
		"repoURL":        o.RepoURL,
		"folderURL":      o.FolderURL,
		"branch":         o.Branch,
		"commitSHA":      o.CommitSHA,
		"templateCommit": o.TemplateCommit,
		"pushed":         o.Pushed,
		"writeMode":      o.WriteMode,
		"repoCreated":    o.RepoCreated,
		"filesTotal":     o.FilesTotal,
		"filesModified":  o.FilesModified,
		"pathsRenamed":   o.PathsRenamed,
		"filesExcluded":  o.FilesExcluded,
		"files":          files,
		"durationMs":     o.DurationMs,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	if o.RepoURL, err = coerce.ToString(values["repoURL"]); err != nil {
		return err
	}
	if o.FolderURL, err = coerce.ToString(values["folderURL"]); err != nil {
		return err
	}
	if o.Branch, err = coerce.ToString(values["branch"]); err != nil {
		return err
	}
	if o.CommitSHA, err = coerce.ToString(values["commitSHA"]); err != nil {
		return err
	}
	if o.TemplateCommit, err = coerce.ToString(values["templateCommit"]); err != nil {
		return err
	}
	if o.Pushed, err = coerce.ToBool(values["pushed"]); err != nil {
		return err
	}
	if o.WriteMode, err = coerce.ToString(values["writeMode"]); err != nil {
		return err
	}
	if o.RepoCreated, err = coerce.ToBool(values["repoCreated"]); err != nil {
		return err
	}
	if o.FilesTotal, err = coerce.ToInt(values["filesTotal"]); err != nil {
		return err
	}
	if o.FilesModified, err = coerce.ToInt(values["filesModified"]); err != nil {
		return err
	}
	if o.PathsRenamed, err = coerce.ToInt(values["pathsRenamed"]); err != nil {
		return err
	}
	if o.FilesExcluded, err = coerce.ToInt(values["filesExcluded"]); err != nil {
		return err
	}
	if o.Files, err = toStrings(values["files"]); err != nil {
		return err
	}
	o.DurationMs, err = coerce.ToInt(values["durationMs"])
	return err
}
