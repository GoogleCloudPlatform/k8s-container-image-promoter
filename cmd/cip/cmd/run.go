/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"
	"os"

	guuid "github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"k8s.io/release/pkg/cip/audit"
	reg "k8s.io/release/pkg/cip/dockerregistry"
	"k8s.io/release/pkg/cip/gcloud"
	"k8s.io/release/pkg/cip/stream"
	"k8s.io/release/pkg/log"
)

var (
	// GitDescribe is stamped by bazel.
	GitDescribe string

	// GitCommit is stamped by bazel.
	GitCommit string

	// TimestampUtcRfc3339 is stamped by bazel.
	TimestampUtcRfc3339 string
)

// rootCmd represents the base command when called without any subcommands
// TODO: Update command description
var rootCmd = &cobra.Command{
	Use:   "cip",
	Short: "Promote images from a staging registry to production",
	Long: `cip - Kubernetes container image promoter
`,
	PersistentPreRunE: initLogging,
	SilenceUsage:      true,
	SilenceErrors:     true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.Wrap(
			runImagePromotion(rootOpts),
			"run `kpromo run images`",
		)
	},
}

// TODO: Push these into a package
type rootOptions struct {
	logLevel                string
	manifest                string
	thinManifestDir         string
	keyFiles                string
	snapshot                string
	snapshotTag             string
	outputFormat            string
	snapshotSvcAcct         string
	manifestBasedSnapshotOf string
	auditManifestRepoURL    string
	auditManifestRepoBranch string
	auditManifestPath       string
	auditGcpProjectID       string
	threads                 int
	maxImageSize            int
	severityThreshold       int
	jsonLogSummary          bool
	parseOnly               bool
	dryRun                  bool
	version                 bool
	minimalSnapshot         bool
	useServiceAcct          bool
	audit                   bool
}

var rootOpts = &rootOptions{}

const (
	// TODO: Push these into a package.
	defaultThreads           = 10
	defaultOutputFormat      = "YAML"
	defaultMaxImageSize      = 2048
	defaultSeverityThreshold = -1

	// flags.
	manifestFlag        = "manifest"
	thinManifestDirFlag = "thin-manifest-dir"
)

// Execute adds all child commands to the root command and sets flags.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		logrus.Fatal(err)
	}
}

// TODO: Function 'init' is too long (171 > 60) (funlen)
// nolint: funlen
func init() {
	rootCmd.PersistentFlags().StringVar(
		&rootOpts.logLevel,
		"log-level",
		"info",
		fmt.Sprintf("the logging verbosity, either %s", log.LevelNames()),
	)

	// TODO: Move this into a default options function in pkg/promobot
	rootCmd.PersistentFlags().StringVar(
		&rootOpts.manifest,
		manifestFlag,
		rootOpts.manifest,
		"the manifest file to load",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.thinManifestDir,
		thinManifestDirFlag,
		rootOpts.thinManifestDir,
		`recursively read in all manifests within a folder, but all manifests
MUST be 'thin' manifests named 'promoter-manifest.yaml', which are like regular
manifests but instead of defining the 'images: ...' field directly, the
'imagesPath' field must be defined that points to another YAML file containing
the 'images: ...' contents`,
	)

	rootCmd.PersistentFlags().IntVar(
		&rootOpts.threads,
		"threads",
		defaultThreads,
		"number of concurrent goroutines to use when talking to GCR",
	)

	rootCmd.PersistentFlags().BoolVar(
		&rootOpts.jsonLogSummary,
		"json-log-summary",
		rootOpts.jsonLogSummary,
		"only log a json summary of important errors",
	)

	rootCmd.PersistentFlags().BoolVar(
		&rootOpts.parseOnly,
		"parse-only",
		rootOpts.parseOnly,
		"only check that the given manifest file is parseable as a Manifest",
	)

	// TODO: Consider moving this to the root command
	rootCmd.PersistentFlags().BoolVar(
		&rootOpts.dryRun,
		"dry-run",
		rootOpts.dryRun,
		"test run promotion without modifying any registry",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.keyFiles,
		"key-files",
		rootOpts.keyFiles,
		`CSV of service account key files that must be activated for the
promotion (<json-key-file-path>,...)`,
	)

	rootCmd.PersistentFlags().BoolVar(
		&rootOpts.version,
		"version",
		rootOpts.version,
		"print version",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.snapshot,
		"snapshot",
		rootOpts.snapshot,
		"read all images in a repository and print to stdout",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.snapshotTag,
		"snapshot-tag",
		rootOpts.snapshotTag,
		"only snapshot images with the given tag",
	)

	rootCmd.PersistentFlags().BoolVar(
		&rootOpts.minimalSnapshot,
		"minimal-snapshot",
		rootOpts.minimalSnapshot,
		`(only works with -snapshot/-manifest-based-snapshot-of) discard tagless
images from snapshot output if they are referenced by a manifest list`,
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.outputFormat,
		"output-format",
		defaultOutputFormat,
		`(only works with -snapshot/-manifest-based-snapshot-of) choose output
format of the snapshot (default: YAML; allowed values: 'YAML' or 'CSV')`,
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.snapshotSvcAcct,
		"snapshot-service-account",
		rootOpts.snapshotSvcAcct,
		"service account to use for -snapshot",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.manifestBasedSnapshotOf,
		"manifest-based-snapshot-of",
		rootOpts.manifestBasedSnapshotOf,
		`read all images in either -manifest or -thin-manifest-dir and print all
images that should be promoted to the given registry (assuming the given
registry is empty); this is like -snapshot, but instead of reading over the
network from a registry, it reads from the local manifests only`,
	)

	rootCmd.PersistentFlags().BoolVar(
		&rootOpts.useServiceAcct,
		"use-service-account",
		rootOpts.useServiceAcct,
		"pass '--account=...' to all gcloud calls (default: false)",
	)

	rootCmd.PersistentFlags().BoolVar(
		&rootOpts.audit,
		"audit",
		rootOpts.audit,
		"stand up an audit server that responds to Pub/Sub push events",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.auditManifestRepoURL,
		"audit-manifest-repo-url",
		// TODO: Set this in a function instead
		os.Getenv("CIP_AUDIT_MANIFEST_REPO_URL"),
		"URL of the repository that holds the promoter manifests",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.auditManifestRepoBranch,
		"audit-manifest-repo-branch",
		// TODO: Set this in a function instead
		os.Getenv("CIP_AUDIT_MANIFEST_REPO_BRANCH"),
		"git branch to check out (use) for --audit-manifest-repo",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.auditManifestPath,
		"audit-manifest-path",
		// TODO: Set this in a function instead
		os.Getenv("CIP_AUDIT_MANIFEST_REPO_MANIFEST_DIR"),
		"relative to the root of `--audit-manifest-repo` to the manifests dir",
	)

	rootCmd.PersistentFlags().StringVar(
		&rootOpts.auditGcpProjectID,
		"audit-gcp-project-id",
		// TODO: Set this in a function instead
		os.Getenv("CIP_AUDIT_GCP_PROJECT_ID"),
		"GCP project ID (name); used for labeling error reporting logs to GCP",
	)

	rootCmd.PersistentFlags().IntVar(
		&rootOpts.maxImageSize,
		"max-image-size",
		defaultMaxImageSize,
		"the maximum image size (in MiB) allowed for promotion",
	)

	// TODO: Set this in a function instead
	if rootOpts.maxImageSize <= 0 {
		rootOpts.maxImageSize = 2048
	}

	rootCmd.PersistentFlags().IntVar(
		&rootOpts.severityThreshold,
		"vuln-severity-threshold",
		defaultSeverityThreshold,
		`Using this flag will cause the promoter to only run the vulnerability
check. Found vulnerabilities at or above this threshold will result in the
vulnerability check failing [severity levels between 0 and 5; 0 - UNSPECIFIED,
1 - MINIMAL, 2 - LOW, 3 - MEDIUM, 4 - HIGH, 5 - CRITICAL]`,
	)
}

// TODO: Function 'runImagePromotion' has too many statements (97 > 40) (funlen)
// nolint: funlen,gocognit,gocyclo
func runImagePromotion(opts *rootOptions) error {
	if opts.version {
		printVersion()
		return nil
	}

	if err := validateImageOptions(opts); err != nil {
		return errors.Wrap(err, "validating image options")
	}

	if opts.audit {
		uuid := os.Getenv("CIP_AUDIT_TESTCASE_UUID")
		if len(uuid) > 0 {
			logrus.Infof("Starting auditor in Test Mode (%s)", uuid)
		} else {
			uuid = guuid.New().String()
			logrus.Infof("Starting auditor in Regular Mode (%s)", uuid)
		}

		auditServerContext, err := audit.InitRealServerContext(
			opts.auditGcpProjectID,
			opts.auditManifestRepoURL,
			opts.auditManifestRepoBranch,
			opts.auditManifestPath,
			uuid,
		)
		if err != nil {
			return errors.Wrap(err, "creating auditor context")
		}

		auditServerContext.RunAuditor()
	}

	// Activate service accounts.
	if opts.useServiceAcct && opts.keyFiles != "" {
		if err := gcloud.ActivateServiceAccounts(opts.keyFiles); err != nil {
			return errors.Wrap(err, "activating service accounts")
		}
	}

	var (
		mfest       reg.Manifest
		srcRegistry *reg.RegistryContext
		err         error
		mfests      []reg.Manifest
	)

	promotionEdges := make(map[reg.PromotionEdge]interface{})
	sc := reg.SyncContext{}
	mi := make(reg.MasterInventory)

	// TODO: Move this into the validation function
	if opts.snapshot != "" || opts.manifestBasedSnapshotOf != "" {
		if opts.snapshot != "" {
			srcRegistry = &reg.RegistryContext{
				Name:           reg.RegistryName(opts.snapshot),
				ServiceAccount: opts.snapshotSvcAcct,
				Src:            true,
			}
		} else {
			srcRegistry = &reg.RegistryContext{
				Name:           reg.RegistryName(opts.manifestBasedSnapshotOf),
				ServiceAccount: opts.snapshotSvcAcct,
				Src:            true,
			}
		}

		mfests = []reg.Manifest{
			{
				Registries: []reg.RegistryContext{
					*srcRegistry,
				},
				Images: []reg.Image{},
			},
		}
		// TODO: Move this into the validation function
	} else if opts.manifest == "" && opts.thinManifestDir == "" {
		logrus.Fatalf(
			"either %s or %s flag is required",
			manifestFlag,
			thinManifestDirFlag,
		)
	}

	doingPromotion := false

	// TODO: is deeply nested (complexity: 5) (nestif)
	// nolint: nestif
	if opts.manifest != "" {
		mfest, err = reg.ParseManifestFromFile(opts.manifest)
		if err != nil {
			logrus.Fatal(err)
		}

		mfests = append(mfests, mfest)
		for _, registry := range mfest.Registries {
			mi[registry.Name] = nil
		}

		sc, err = reg.MakeSyncContext(
			mfests,
			opts.threads,
			opts.dryRun,
			opts.useServiceAcct,
		)
		if err != nil {
			logrus.Fatal(err)
		}

		doingPromotion = true
	} else if opts.thinManifestDir != "" {
		mfests, err = reg.ParseThinManifestsFromDir(opts.thinManifestDir)
		if err != nil {
			return errors.Wrap(err, "parsing thin manifest directory")
		}

		sc, err = reg.MakeSyncContext(
			mfests,
			opts.threads,
			opts.dryRun,
			opts.useServiceAcct)
		if err != nil {
			logrus.Fatal(err)
		}

		doingPromotion = true
	}

	if opts.parseOnly {
		return nil
	}

	// If there are no images in the manifest, it may be a stub manifest file
	// (such as for brand new registries that would be watched by the promoter
	// for the very first time).
	// TODO: is deeply nested (complexity: 6) (nestif)
	// nolint: nestif
	if doingPromotion && opts.manifestBasedSnapshotOf == "" {
		promotionEdges, err = reg.ToPromotionEdges(mfests)
		if err != nil {
			return errors.Wrap(
				err,
				"converting list of manifests to edges for promotion",
			)
		}

		imagesInManifests := false
		for _, mfest := range mfests {
			if len(mfest.Images) > 0 {
				imagesInManifests = true
				break
			}
		}
		if !imagesInManifests {
			logrus.Info("No images in manifest(s) --- nothing to do.")
			return nil
		}

		// Print version to make Prow logs more self-explanatory.
		printVersion()

		// nolint: gocritic
		if opts.severityThreshold >= 0 {
			logrus.Info("********** START (VULN CHECK) **********")
			logrus.Info(
				`DISCLAIMER: Vulnerabilities are found as issues with package
binaries within image layers, not necessarily with the image layers themselves.
So a 'fixable' vulnerability may not necessarily be immediately actionable. For
example, even though a fixed version of the binary is available, it doesn't
necessarily mean that a new version of the image layer is available.`,
			)
		} else if opts.dryRun {
			logrus.Info("********** START (DRY RUN) **********")
		} else {
			logrus.Info("********** START **********")
		}
	}

	// TODO: is deeply nested (complexity: 12) (nestif)
	// nolint: nestif
	if len(opts.snapshot) > 0 || len(opts.manifestBasedSnapshotOf) > 0 {
		rii := make(reg.RegInvImage)
		if len(opts.manifestBasedSnapshotOf) > 0 {
			promotionEdges, err = reg.ToPromotionEdges(mfests)
			if err != nil {
				return errors.Wrap(
					err,
					"converting list of manifests to edges for promotion",
				)
			}

			rii = reg.EdgesToRegInvImage(
				promotionEdges,
				opts.manifestBasedSnapshotOf,
			)

			if opts.minimalSnapshot {
				sc.ReadRegistries(
					[]reg.RegistryContext{*srcRegistry},
					true,
					reg.MkReadRepositoryCmdReal,
				)

				sc.ReadGCRManifestLists(reg.MkReadManifestListCmdReal)
				rii = sc.RemoveChildDigestEntries(rii)
			}
		} else {
			sc, err = reg.MakeSyncContext(
				mfests,
				opts.threads,
				opts.dryRun,
				opts.useServiceAcct,
			)
			if err != nil {
				logrus.Fatal(err)
			}

			sc.ReadRegistries(
				[]reg.RegistryContext{*srcRegistry},
				// Read all registries recursively, because we want to produce a
				// complete snapshot.
				true,
				reg.MkReadRepositoryCmdReal,
			)

			rii = sc.Inv[mfests[0].Registries[0].Name]
			if opts.snapshotTag != "" {
				rii = reg.FilterByTag(rii, opts.snapshotTag)
			}

			if opts.minimalSnapshot {
				logrus.Info("removing tagless child digests of manifest lists")
				sc.ReadGCRManifestLists(reg.MkReadManifestListCmdReal)
				rii = sc.RemoveChildDigestEntries(rii)
			}
		}

		var snapshot string
		switch opts.outputFormat {
		case "CSV":
			snapshot = rii.ToCSV()
		case "YAML":
			snapshot = rii.ToYAML(reg.YamlMarshalingOpts{})
		default:
			logrus.Errorf(
				"invalid value %s for -output-format; defaulting to YAML",
				opts.outputFormat,
			)

			snapshot = rii.ToYAML(reg.YamlMarshalingOpts{})
		}

		fmt.Print(snapshot)
		return nil
	}

	if opts.jsonLogSummary {
		defer sc.LogJSONSummary()
	}

	// Check the pull request
	if opts.dryRun {
		err = sc.RunChecks([]reg.PreCheck{})
		if err != nil {
			return errors.Wrap(err, "running prechecks before promotion")
		}
	}

	// Promote.
	mkProducer := func(
		srcRegistry reg.RegistryName,
		srcImageName reg.ImageName,
		destRC reg.RegistryContext,
		imageName reg.ImageName,
		digest reg.Digest, tag reg.Tag, tp reg.TagOp,
	) stream.Producer {
		var sp stream.Subprocess
		sp.CmdInvocation = reg.GetWriteCmd(
			destRC,
			sc.UseServiceAccount,
			srcRegistry,
			srcImageName,
			imageName,
			digest,
			tag,
			tp,
		)

		return &sp
	}

	promotionEdges, ok := sc.FilterPromotionEdges(promotionEdges, true)
	// If any funny business was detected during a comparison of the manifests
	// with the state of the registries, then exit immediately.
	if !ok {
		return errors.New("encountered errors during edge filtering")
	}

	if opts.severityThreshold >= 0 {
		err = sc.RunChecks(
			[]reg.PreCheck{
				reg.MKImageVulnCheck(
					sc,
					promotionEdges,
					opts.severityThreshold,
					nil,
				),
			},
		)
		if err != nil {
			return errors.Wrap(err, "checking image vulnerabilities")
		}
	} else {
		err = sc.Promote(promotionEdges, mkProducer, nil)
		if err != nil {
			return errors.Wrap(err, "promoting images")
		}
	}

	// nolint: gocritic
	if opts.severityThreshold >= 0 {
		logrus.Info("********** FINISHED (VULN CHECK) **********")
	} else if opts.dryRun {
		logrus.Info("********** FINISHED (DRY RUN) **********")
	} else {
		logrus.Info("********** FINISHED **********")
	}

	return nil
}

func initLogging(*cobra.Command, []string) error {
	return log.SetupGlobalLogger(rootOpts.logLevel)
}

func validateImageOptions(o *rootOptions) error {
	// TODO: Validate options
	return nil
}

func printVersion() {
	fmt.Printf("Built:   %s\n", TimestampUtcRfc3339)
	fmt.Printf("Version: %s\n", GitDescribe)
	fmt.Printf("Commit:  %s\n", GitCommit)
}