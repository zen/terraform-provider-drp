package main

import (
    "github.com/hashicorp/terraform-plugin-sdk/plugin"
 	"github.com/rackn/terraform-provider-drpv4/drpv4"
)

func main() {
    plugin.Serve(&plugin.ServeOpts{ProviderFunc: drpv4.Provider})
}