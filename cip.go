/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"os"

	// nolint[lll]
	"k8s.io/klog"
	reg "sigs.k8s.io/k8s-container-image-promoter/lib/dockerregistry"
	"sigs.k8s.io/k8s-container-image-promoter/lib/stream"
	"sigs.k8s.io/k8s-container-image-promoter/pkg/gcloud"
)

// GitDescribe is stamped by bazel.
var GitDescribe string

// GitCommit is stamped by bazel.
var GitCommit string

// TimestampUtcRfc3339 is stamped by bazel.
var TimestampUtcRfc3339 string

// nolint[gocyclo]
func main() {
	klog.InitFlags(nil)

	manifestPtr := flag.String(
		"manifest", "", "the manifest file to load")
	manifestDirPtr := flag.String(
		"manifest-dir",
		"",
		"(DEPRECATED; please use -thin-manifest-dir instead) recursively read in all manifests within a folder; it is an error if two manifests specify conflicting intent (e.g., promotion of the same image); manifests inside this directory *MUST* be named 'promoter-manifest.yaml'")
	thinManifestDirPtr := flag.String(
		"thin-manifest-dir",
		"",
		"recursively read in all manifests within a folder, but all manifests MUST be 'thin' manifests named 'promoter-manifest.yaml', which are like regular manifests but instead of defining the 'images: ...' field directly, the 'imagesPath' field must be defined that points to another YAML file containing the 'images: ...' contents")
	threadsPtr := flag.Int(
		"threads",
		10, "number of concurrent goroutines to use when talking to GCR")
	verbosityPtr := flag.Int(
		"verbosity",
		2,
		"verbosity level for logging;"+
			" 0 = fatal only,"+
			" 1 = fatal + errors,"+
			" 2 = fatal + errors + warnings,"+
			" 3 = fatal + errors + warnings + informational (everything)")
	parseOnlyPtr := flag.Bool(
		"parse-only",
		false,
		"only check that the given manifest file is parseable as a Manifest"+
			" (default: false)")
	dryRunPtr := flag.Bool(
		"dry-run",
		true,
		"print what would have happened by running this tool;"+
			" do not actually modify any registry")
	keyFilesPtr := flag.String(
		"key-files",
		"",
		"CSV of service account key files that must be activated for the promotion (<json-key-file-path>,...)")
	// Add in help flag information, because Go's "flag" package automatically
	// adds it, but for whatever reason does not show it as part of available
	// options.
	helpPtr := flag.Bool(
		"help",
		false,
		"print help")
	versionPtr := flag.Bool(
		"version",
		false,
		"print version")
	snapshotPtr := flag.String(
		"snapshot",
		"",
		"read all images in a repository and print to stdout")
	snapshotTag := ""
	flag.StringVar(&snapshotTag, "snapshot-tag", snapshotTag, "only snapshot images with the given tag")
	minimalSnapshotPtr := flag.Bool(
		"minimal-snapshot",
		false,
		"(only works with -snapshot/-manifest-based-snapshot-of) discard tagless images from snapshot output if they are referenced by a manifest list")
	outputFormatPtr := flag.String(
		"output-format",
		"YAML",
		"(only works with -snapshot/-manifest-based-snapshot-of) choose output format of the snapshot (default: YAML; allowed values: 'YAML' or 'CSV')")
	snapshotSvcAccPtr := flag.String(
		"snapshot-service-account",
		"",
		"service account to use for -snapshot")
	manifestBasedSnapshotOf := flag.String(
		"manifest-based-snapshot-of",
		"",
		"read all images in either -manifest or -manifest-dir and print all images that will be promoted to the given registry; this is like -snapshot, but instead of reading from a registry, it reads from the manifests those images that need to be promoted to the given registry")
	noSvcAcc := false
	flag.BoolVar(&noSvcAcc, "no-service-account", false,
		"do not pass '--account=...' to all gcloud calls (default: false)")
	flag.Parse()

	if len(os.Args) == 1 {
		printVersion()
		printUsage()
		os.Exit(0)
	}

	if *helpPtr {
		printUsage()
		os.Exit(0)
	}

	if *versionPtr {
		printVersion()
		os.Exit(0)
	}

	// Activate service accounts.
	if len(*keyFilesPtr) > 0 {
		if err := gcloud.ActivateServiceAccounts(*keyFilesPtr); err != nil {
			klog.Exitln(err)
		}
	}

	var mfest reg.Manifest
	var srcRegistry *reg.RegistryContext
	var err error
	var mfests []reg.Manifest
	promotionEdges := make(map[reg.PromotionEdge]interface{})
	sc := reg.SyncContext{}
	mi := make(reg.MasterInventory)

	if len(*snapshotPtr) > 0 || len(*manifestBasedSnapshotOf) > 0 {
		if len(*snapshotPtr) > 0 {
			srcRegistry = &reg.RegistryContext{
				Name:           reg.RegistryName(*snapshotPtr),
				ServiceAccount: *snapshotSvcAccPtr,
				Src:            true,
			}
		} else {
			srcRegistry = &reg.RegistryContext{
				Name:           reg.RegistryName(*manifestBasedSnapshotOf),
				ServiceAccount: *snapshotSvcAccPtr,
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
	} else {
		if *manifestPtr == "" && *manifestDirPtr == "" && *thinManifestDirPtr == "" {
			klog.Fatal(fmt.Errorf("one of -manifest, -manifest-dir, or -thin-manifest-dir is required"))
		}
	}

	doingPromotion := false
	if *manifestPtr != "" {
		mfest, err = reg.ParseManifestFromFile(*manifestPtr, "")
		if err != nil {
			klog.Fatal(err)
		}
		mfests = append(mfests, mfest)
		for _, registry := range mfest.Registries {
			mi[registry.Name] = nil
		}
		sc, err = reg.MakeSyncContext(
			mfests,
			*verbosityPtr,
			*threadsPtr,
			*dryRunPtr,
			!noSvcAcc)
		if err != nil {
			klog.Fatal(err)
		}
		doingPromotion = true
	} else if *manifestDirPtr != "" || *thinManifestDirPtr != "" {
		if *manifestDirPtr != "" {
			mfests, err = reg.ParseManifestsFromDir(*manifestDirPtr, reg.ParseManifestFromFile)
		} else {
			mfests, err = reg.ParseManifestsFromDir(*thinManifestDirPtr, reg.ParseThinManifestFromFile)
		}
		if err != nil {
			klog.Exitln(err)
		}

		sc, err = reg.MakeSyncContext(
			mfests,
			*verbosityPtr,
			*threadsPtr,
			*dryRunPtr,
			!noSvcAcc)
		if err != nil {
			klog.Fatal(err)
		}
		doingPromotion = true
	}

	if *parseOnlyPtr {
		os.Exit(0)
	}

	// If there are no images in the manifest, it may be a stub manifest file
	// (such as for brand new registries that would be watched by the promoter
	// for the very first time).
	if doingPromotion && len(*manifestBasedSnapshotOf) == 0 {
		promotionEdges, err = reg.ToPromotionEdges(mfests)
		if err != nil {
			klog.Exitln(err)
		}

		imagesInManifests := false
		for _, mfest := range mfests {
			if len(mfest.Images) > 0 {
				imagesInManifests = true
				break
			}
		}
		if !imagesInManifests {
			klog.Info("No images in manifest(s) --- nothing to do.")
			os.Exit(0)
		}

		// Print version to make Prow logs more self-explanatory.
		printVersion()

		if *dryRunPtr {
			klog.Info("********** START (DRY RUN) **********")
		} else {
			klog.Info("********** START **********")
		}
	}

	if len(*snapshotPtr) > 0 || len(*manifestBasedSnapshotOf) > 0 {
		rii := make(reg.RegInvImage)
		if len(*manifestBasedSnapshotOf) > 0 {
			promotionEdges, err = reg.ToPromotionEdges(mfests)
			if err != nil {
				klog.Exitln(err)
			}
			rii = reg.EdgesToRegInvImage(promotionEdges,
				*manifestBasedSnapshotOf)

			if *minimalSnapshotPtr {
				sc.ReadRegistries(
					[]reg.RegistryContext{*srcRegistry},
					true,
					reg.MkReadRepositoryCmdReal)
				sc.ReadGCRManifestLists(reg.MkReadManifestListCmdReal)
				rii = sc.RemoveChildDigestEntries(rii)
			}
		} else {
			sc, err = reg.MakeSyncContext(
				mfests,
				*verbosityPtr,
				*threadsPtr,
				*dryRunPtr,
				!noSvcAcc)
			if err != nil {
				klog.Fatal(err)
			}
			sc.ReadRegistries(
				[]reg.RegistryContext{*srcRegistry},
				// Read all registries recursively, because we want to produce a
				// complete snapshot.
				true,
				reg.MkReadRepositoryCmdReal)

			rii = sc.Inv[mfests[0].Registries[0].Name]
			if snapshotTag != "" {
				rii = reg.FilterByTag(rii, snapshotTag)
			}
			if *minimalSnapshotPtr {
				klog.Info("-minimal-snapshot specifed; removing tagless child digests of manifest lists")
				sc.ReadGCRManifestLists(reg.MkReadManifestListCmdReal)
				rii = sc.RemoveChildDigestEntries(rii)
			}
		}

		var snapshot string
		switch *outputFormatPtr {
		case "CSV":
			snapshot = rii.ToCSV()
		case "YAML":
			snapshot = rii.ToYAML()
		default:
			klog.Errorf("invalid value %s for -output-format; defaulting to YAML", *outputFormatPtr)
			snapshot = rii.ToYAML()
		}
		fmt.Print(snapshot)
		os.Exit(0)
	}

	// Promote.
	mkProducer := func(
		srcRegistry reg.RegistryName,
		srcImageName reg.ImageName,
		destRC reg.RegistryContext,
		imageName reg.ImageName,
		digest reg.Digest, tag reg.Tag, tp reg.TagOp) stream.Producer {
		var sp stream.Subprocess
		sp.CmdInvocation = reg.GetWriteCmd(
			destRC,
			sc.UseServiceAccount,
			srcRegistry,
			srcImageName,
			imageName,
			digest,
			tag,
			tp)
		return &sp
	}
	promotionEdges = sc.FilterPromotionEdges(promotionEdges, true)
	err = sc.Promote(promotionEdges, mkProducer, nil)
	if err != nil {
		klog.Exitln(err)
	}

	if *dryRunPtr {
		klog.Info("********** FINISHED (DRY RUN) **********")
	} else {
		klog.Info("********** FINISHED **********")
	}
}

func printVersion() {
	fmt.Printf("Built:   %s\n", TimestampUtcRfc3339)
	fmt.Printf("Version: %s\n", GitDescribe)
	fmt.Printf("Commit:  %s\n", GitCommit)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	flag.PrintDefaults()
}
