package vault_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/hashicorp/vault/api"
	credLdap "github.com/hashicorp/vault/builtin/credential/ldap"
	vaulthttp "github.com/hashicorp/vault/http"
	"github.com/hashicorp/vault/logical"
	"github.com/hashicorp/vault/vault"
)

func TestTokenStore_IdentityPolicies(t *testing.T) {
	coreConfig := &vault.CoreConfig{
		CredentialBackends: map[string]logical.Factory{
			"ldap": credLdap.Factory,
		},
	}
	cluster := vault.NewTestCluster(t, coreConfig, &vault.TestClusterOptions{
		HandlerFunc: vaulthttp.Handler,
	})
	cluster.Start()
	defer cluster.Cleanup()

	core := cluster.Cores[0].Core
	vault.TestWaitActive(t, core)
	client := cluster.Cores[0].Client

	err := client.Sys().EnableAuthWithOptions("ldap", &api.EnableAuthOptions{
		Type: "ldap",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Logical().Write("auth/ldap/config", map[string]interface{}{
		"url":      "ldap://ldap.forumsys.com",
		"userattr": "uid",
		"userdn":   "dc=example,dc=com",
		"groupdn":  "dc=example,dc=com",
		"binddn":   "cn=read-only-admin,dc=example,dc=com",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Logical().Write("auth/ldap/groups/testgroup1", map[string]interface{}{
		"policies": "testgroup1-policy",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Logical().Write("auth/ldap/users/tesla", map[string]interface{}{
		"policies": "default",
		"groups":   "testgroup1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Login using LDAP
	secret, err := client.Logical().Write("auth/ldap/login/tesla", map[string]interface{}{
		"password": "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	ldapClientToken := secret.Auth.ClientToken

	secret, err = client.Logical().Read("auth/token/lookup/" + ldapClientToken)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := secret.Data["identity_policies"]
	if ok {
		t.Fatalf("identity_policies should not have been set")
	}

	entityID := secret.Data["entity_id"].(string)

	_, err = client.Logical().Write("identity/entity/id/"+entityID, map[string]interface{}{
		"policies": []string{
			"entity_policy_1",
			"entity_policy_2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	secret, err = client.Logical().Read("auth/token/lookup/" + ldapClientToken)
	if err != nil {
		t.Fatal(err)
	}
	identityPolicies := secret.Data["identity_policies"].([]interface{})
	var actualPolicies []string
	for _, item := range identityPolicies {
		actualPolicies = append(actualPolicies, item.(string))
	}
	sort.Strings(actualPolicies)

	expectedPolicies := []string{
		"entity_policy_1",
		"entity_policy_2",
	}
	sort.Strings(expectedPolicies)
	if !reflect.DeepEqual(expectedPolicies, actualPolicies) {
		t.Fatalf("bad: identity policies; expected: %#v\nactual: %#v", expectedPolicies, actualPolicies)
	}

	secret, err = client.Logical().Write("identity/group", map[string]interface{}{
		"policies": []string{
			"group_policy_1",
			"group_policy_2",
		},
		"member_entity_ids": []string{
			entityID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	secret, err = client.Logical().Read("auth/token/lookup/" + ldapClientToken)
	if err != nil {
		t.Fatal(err)
	}
	identityPolicies = secret.Data["identity_policies"].([]interface{})
	actualPolicies = nil
	for _, item := range identityPolicies {
		actualPolicies = append(actualPolicies, item.(string))
	}
	sort.Strings(actualPolicies)

	expectedPolicies = []string{
		"entity_policy_1",
		"entity_policy_2",
		"group_policy_1",
		"group_policy_2",
	}
	sort.Strings(expectedPolicies)
	if !reflect.DeepEqual(expectedPolicies, actualPolicies) {
		t.Fatalf("bad: identity policies; expected: %#v\nactual: %#v", expectedPolicies, actualPolicies)
	}

	// Extract out the mount accessor for LDAP auth
	auths, err := client.Sys().ListAuth()
	if err != nil {
		t.Fatal(err)
	}
	ldapMountAccessor1 := auths["ldap/"].Accessor

	// Create an external group
	secret, err = client.Logical().Write("identity/group", map[string]interface{}{
		"type": "external",
		"policies": []string{
			"external_group_policy_1",
			"external_group_policy_2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ldapExtGroupID1 := secret.Data["id"].(string)

	// Associate a group from LDAP auth as a group-alias in the external group
	_, err = client.Logical().Write("identity/group-alias", map[string]interface{}{
		"name":           "testgroup1",
		"mount_accessor": ldapMountAccessor1,
		"canonical_id":   ldapExtGroupID1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Renew token to refresh external group memberships
	secret, err = client.Auth().Token().Renew(ldapClientToken, 10)
	if err != nil {
		t.Fatal(err)
	}

	secret, err = client.Logical().Read("auth/token/lookup/" + ldapClientToken)
	if err != nil {
		t.Fatal(err)
	}
	identityPolicies = secret.Data["identity_policies"].([]interface{})
	actualPolicies = nil
	for _, item := range identityPolicies {
		actualPolicies = append(actualPolicies, item.(string))
	}
	sort.Strings(actualPolicies)
	fmt.Printf("actualPolicies: %#v\n", actualPolicies)

	expectedPolicies = []string{
		"entity_policy_1",
		"entity_policy_2",
		"group_policy_1",
		"group_policy_2",
		"external_group_policy_1",
		"external_group_policy_2",
	}
	sort.Strings(expectedPolicies)
	if !reflect.DeepEqual(expectedPolicies, actualPolicies) {
		t.Fatalf("bad: identity policies; expected: %#v\nactual: %#v", expectedPolicies, actualPolicies)
	}
}
