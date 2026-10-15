package gitreposcaffolder

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/support/log"
)

// Flogo seeds unmapped inputs from these struct values, so they must carry the defaults.
var activityMd = activity.ToMetadata(newInput(), &Output{})

var nowFunc = time.Now

func init() {
	_ = activity.Register(&Activity{}, New)
}

// Error codes surface in the flow's error handler as $error.code.
const (
	ErrCodeConfig     = "GITSCAFFOLD-4001"
	ErrCodeTemplate   = "GITSCAFFOLD-4002"
	ErrCodeTransform  = "GITSCAFFOLD-4003"
	ErrCodeRepoCreate = "GITSCAFFOLD-4004"
	ErrCodeRepoExists = "GITSCAFFOLD-4005"
	ErrCodeWrite      = "GITSCAFFOLD-4006"
	ErrCodeConflict   = "GITSCAFFOLD-4007"
)

type Activity struct{}

func New(_ activity.InitContext) (activity.Activity, error) {
	return &Activity{}, nil
}

func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	logger := ctx.Logger()
	start := nowFunc()

	in := newInput()
	if err := ctx.GetInputObject(in); err != nil {
		return false, activity.NewError(err.Error(), ErrCodeConfig, nil)
	}
	out, err := run(context.Background(), in, logger)
	if err != nil {
		return false, err
	}
	out.DurationMs = int(nowFunc().Sub(start).Milliseconds())
	logger.Infof("Git Repo Scaffolder: %d file(s) [modified %d, renamed %d, excluded %d] -> %s (pushed=%t, commit=%s) in %d ms",
		out.FilesTotal, out.FilesModified, out.PathsRenamed, out.FilesExcluded, out.FolderURL, out.Pushed, short(out.CommitSHA), out.DurationMs)
	if err := ctx.SetOutputObject(out); err != nil {
		return false, err
	}
	return true, nil
}

func validate(in *Input) (RepoRef, string, error) {
	if strings.TrimSpace(in.TemplateRepoURL) == "" {
		return RepoRef{}, "", errors.New("templateRepoURL is required")
	}
	if in.TargetMode != TargetModeSubfolder && in.TargetMode != TargetModeNewRepo {
		return RepoRef{}, "", fmt.Errorf("targetMode must be %q or %q", TargetModeSubfolder, TargetModeNewRepo)
	}
	if in.WriteStrategy != WriteStrategyAuto && in.WriteStrategy != WriteStrategyAPI && in.WriteStrategy != WriteStrategyGit {
		return RepoRef{}, "", fmt.Errorf("writeStrategy must be %q, %q or %q", WriteStrategyAuto, WriteStrategyAPI, WriteStrategyGit)
	}
	if in.DryRun && strings.TrimSpace(in.TargetRepoURL) == "" {
		return RepoRef{}, "", nil
	}
	target, err := ParseRepoURL(in.TargetRepoURL)
	if err != nil {
		return RepoRef{}, "", fmt.Errorf("targetRepoURL: %w", err)
	}
	folder := strings.Trim(path.Clean("/"+strings.ReplaceAll(in.TargetFolder, "\\", "/")), "/")
	if in.TargetMode == TargetModeSubfolder && folder == "" {
		return RepoRef{}, "", errors.New("targetFolder is required in subfolder mode (it would otherwise replace the whole shared repository)")
	}
	if strings.TrimSpace(in.TargetBranch) == "" {
		return RepoRef{}, "", errors.New("targetBranch is required")
	}
	return target, folder, nil
}

func run(parent context.Context, in *Input, logger log.Logger) (*Output, error) {
	target, folder, err := validate(in)
	if err != nil {
		return nil, activity.NewError(err.Error(), ErrCodeConfig, nil)
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(in.TimeoutSeconds)*time.Second)
	defer cancel()

	tplToken := in.TemplateAuthToken
	if tplToken == "" {
		tplToken = in.AuthToken
	}
	tplAuth := basicAuth(tplToken)
	if !strings.HasPrefix(strings.ToLower(in.TemplateRepoURL), "http") {
		tplAuth = nil
	}
	files, tplCommit, err := FetchTemplate(ctx, in.TemplateRepoURL, in.TemplateRef, in.TemplatePath, tplAuth)
	if err != nil {
		return nil, activity.NewError("template: "+err.Error(), ErrCodeTemplate, nil)
	}

	repl := in.Replacements
	if in.IgnoreEmptyValues {
		repl = make(map[string]string, len(in.Replacements))
		for k, v := range in.Replacements {
			if v != "" {
				repl[k] = v
			}
		}
	}
	res, err := Transform(files, TransformOptions{
		Replacements:     repl,
		Mode:             in.ReplacementMode,
		RenamePaths:      in.RenamePaths,
		ExcludePatterns:  in.ExcludePatterns,
		BinaryExtensions: in.BinaryExtensions,
		MaxTextBytes:     int64(in.MaxTextBytes),
	})
	if err != nil {
		return nil, activity.NewError(err.Error(), ErrCodeTransform, nil)
	}
	extra, err := toExtraFiles(in.ExtraFiles)
	if err == nil {
		err = placeFiles(res, in.OutputPrefix, extra)
	}
	if err != nil {
		return nil, activity.NewError(err.Error(), ErrCodeConfig, nil)
	}

	out := &Output{
		Branch:         in.TargetBranch,
		TemplateCommit: tplCommit,
		FilesTotal:     len(res.Files),
		FilesModified:  res.FilesModified,
		PathsRenamed:   res.PathsRenamed,
		FilesExcluded:  res.FilesExcluded,
		Files:          make([]string, len(res.Files)),
	}
	for i, f := range res.Files {
		out.Files[i] = path.Join(folder, f.Path)
	}
	if target.Name != "" {
		out.RepoURL = target.WebURL()
		out.FolderURL = out.RepoURL
		if folder != "" {
			out.FolderURL = out.RepoURL + "/tree/" + in.TargetBranch + "/" + folder
		}
	}
	if in.DryRun {
		logger.Debugf("Git Repo Scaffolder: dry run, nothing written")
		return out, nil
	}

	if in.TargetMode == TargetModeNewRepo {
		api := githubAPI{base: in.APIBaseURL, token: in.AuthToken}
		created, err := api.EnsureRepo(ctx, target, in.NewRepoPrivate, in.NewRepoDesc, in.FailIfRepoExists)
		if err != nil {
			code := ErrCodeRepoCreate
			if errors.Is(err, errRepoExists) {
				code = ErrCodeRepoExists
			}
			return nil, activity.NewError(err.Error(), code, nil)
		}
		out.RepoCreated = created
	}

	opts := WriteOptions{
		URL:         "https://" + target.Host + "/" + target.Owner + "/" + target.Name + ".git",
		Branch:      in.TargetBranch,
		Folder:      folder,
		CleanFolder: in.CleanTargetFolder,
		Message:     in.CommitMessage,
		AuthorName:  in.AuthorName,
		AuthorEmail: in.AuthorEmail,
		Auth:        basicAuth(in.AuthToken),
		Retries:     in.PushRetries,
	}
	api := githubAPI{base: in.APIBaseURL, token: in.AuthToken}
	useGit := !useAPI(in, target)
	var wr *WriteResult
	if !useGit {
		wr, err = api.WriteFilesAPI(ctx, target, res.Files, opts)
		if errors.Is(err, errAPINotApplicable) && in.WriteStrategy == WriteStrategyAuto {
			logger.Debugf("Git Repo Scaffolder: %v, using git transport", err)
			useGit, err = true, nil
		}
	}
	if useGit {
		wr, err = WriteFiles(ctx, res.Files, opts)
	}
	if err != nil {
		code := ErrCodeWrite
		if errors.Is(err, errPushConflict) {
			code = ErrCodeConflict
		}
		return nil, activity.NewError(err.Error(), code, nil)
	}
	out.WriteMode = WriteStrategyAPI
	if useGit {
		out.WriteMode = WriteStrategyGit
	}
	out.CommitSHA = wr.CommitSHA
	out.Pushed = wr.Pushed
	if out.RepoCreated && wr.Pushed {
		if err := api.SetDefaultBranch(ctx, target, in.TargetBranch); err != nil {
			logger.Warnf("Git Repo Scaffolder: files pushed, but %v", err)
		}
	}
	return out, nil
}

// useAPI: auto only uses the Git Data API where a GitHub API is known to exist.
func useAPI(in *Input, target RepoRef) bool {
	switch in.WriteStrategy {
	case WriteStrategyAPI:
		return true
	case WriteStrategyGit:
		return false
	}
	return strings.EqualFold(target.Host, "github.com") || strings.TrimRight(in.APIBaseURL, "/") != defaultAPIBaseURL
}
