/*
Copyright 2023 TV 2 DANMARK A/S

Licensed under the Apache License, Version 2.0 (the "License") with the
following modification to section 6. Trademarks:

Section 6. Trademarks is deleted and replaced by the following wording:

6. Trademarks. This License does not grant permission to use the trademarks and
trade names of TV 2 DANMARK A/S, including but not limited to the TV 2® logo and
word mark, except (a) as required for reasonable and customary use in describing
the origin of the Work, e.g. as described in section 4(c) of the License, and
(b) to reproduce the content of the NOTICE file. Any reference to the Licensor
must be made by making a reference to "TV 2 DANMARK A/S", written in capitalized
letters as in this example, unless the format in which the reference is made,
requires lower case letters.

You may not use this software except in compliance with the License and the
modifications set out above.

You may obtain a copy of the license at:

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package unitsuite

import (
	"embed"
	"os"
	"testing"

	"github.com/tv2/bifrost-gateway-controller/test/unit/tests"

	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/gateway-api/conformance"
	"sigs.k8s.io/gateway-api/conformance/utils/flags"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

//go:embed tests/*.yaml base/*.yaml
var Manifests embed.FS

func TestUnit(t *testing.T) {
	if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
		t.Skipf("Skipping unit-tests - requires an external cluster")
	}

	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatalf("Error loading Kubernetes config: %v", err)
	}
	client, err := runtimeclient.New(cfg, runtimeclient.Options{})
	if err != nil {
		t.Fatalf("Error initializing Kubernetes client: %v", err)
	}
	err = v1alpha2.AddToScheme(client.Scheme())
	if err != nil {
		t.Fatalf("Error adding scheme: %v", err)
	}
	err = v1beta1.AddToScheme(client.Scheme())
	if err != nil {
		t.Fatalf("Error adding scheme: %v", err)
	}

	supportedFeatures := suite.ParseSupportedFeatures(*flags.SupportedFeatures)
	exemptFeatures := suite.ParseSupportedFeatures(*flags.ExemptFeatures)
	for feature := range exemptFeatures {
		supportedFeatures.Delete(feature)
	}

	t.Logf("Running conformance tests with %s GatewayClass\n cleanup: %t\n debug: %t\n supported features: [%v]\n exempt features: [%v]\n num tests: %v",
		*flags.GatewayClassName, *flags.CleanupBaseResources, *flags.ShowDebug, *flags.SupportedFeatures, *flags.ExemptFeatures, len(tests.UnitTests))

	conformance.Manifests = Manifests
	unitSuite, err := suite.NewConformanceTestSuite(suite.ConformanceOptions{
		Client:               client,
		GatewayClassName:     *flags.GatewayClassName,
		Debug:                *flags.ShowDebug,
		CleanupBaseResources: *flags.CleanupBaseResources,
		SupportedFeatures:    supportedFeatures,
	})
	if err != nil {
		t.Fatalf("Error creating suite: %v", err)
	}
	unitSuite.Setup(t, tests.UnitTests)
	unitSuite.Run(t, tests.UnitTests)
}
