package drpv4
/*
 * Copyright RackN 2020
 */

import (
	"log"
	"fmt"
	"strings"
    "github.com/hashicorp/terraform-plugin-sdk/helper/schema"
    "github.com/hashicorp/terraform-plugin-sdk/terraform"
)

/*
 * Enable terraform to use DRP as a provider.  Fill out the
 * appropriate functions and information about this plugin.
 */
func Provider() terraform.ResourceProvider {
	log.Println("[DEBUG] Initializing the DRP provider")
	provider := &schema.Provider{

        ResourcesMap: map[string]*schema.Resource{
                "drp_machine": resourceMachine(),
        },

		Schema: map[string]*schema.Schema{
			"token": {
				Type:     schema.TypeString,
				Optional: true,
				Description: "Granted DRP token (use instead of RS_KEY",
				DefaultFunc: schema.MultiEnvDefaultFunc([]string{
					"RS_TOKEN",
				}, nil),
				ConflictsWith: []string{"key","password"},
			},
			"key": {
				Type:     schema.TypeString,
				Optional: true,
				Description: "The DRP user:password key",
				DefaultFunc: schema.MultiEnvDefaultFunc([]string{
					"RS_KEY",
				}, nil),
				ConflictsWith: []string{"token"},
			},
			"username": {
				Type:     schema.TypeString,
				Optional: true,
				Description: "The DRP user",
				ConflictsWith: []string{"key"},
			},
			"password": {
				Type:     schema.TypeString,
				Optional: true,
				Description: "The DRP password",
				ConflictsWith: []string{"key","token"},
			},
			"endpoint": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The DRP server URL. ie: https://1.2.3.4:8092",
				DefaultFunc: schema.MultiEnvDefaultFunc([]string{
					"RS_ENDPOINT",
				}, nil),
			},
		},
	}
	provider.ConfigureFunc = func(d *schema.ResourceData) (interface{}, error) {
		terraformVersion := provider.TerraformVersion
		if terraformVersion == "" {
			// Terraform 0.12 introduced this field to the protocol
			// We can therefore assume that if it's missing it's 0.10 or 0.11
			terraformVersion = "0.11+compatible"
		}
		return providerConfigure(d, provider, terraformVersion)
	}
	return provider
}


/*
 * The config method that terraform uses to pass information about configuration
 * to the plugin.
 */
func providerConfigure(d *schema.ResourceData, p *schema.Provider, terraformVersion string) (interface{}, error) {
	log.Println("[DEBUG] Configuring the DRP provider")
	config := Config{
		endpoint:        d.Get("endpoint").(string),
		username:		 d.Get("username").(string),
		password:        d.Get("password").(string),
	}

	if token := d.Get("token"); token != nil {
		config.token = token.(string)
	}
	if key := d.Get("key"); key != "" {
		parts := strings.SplitN(key.(string), ":", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("RS_KEY (%s) has not enough parts", key)
		}
		config.username = parts[0]
		config.password = parts[1]
	}

	if config.token == "" && config.username == "" {
		return nil, fmt.Errorf("drp provider requires username/password, credential, or token")
	}
	if config.username != "" && config.password == "" {
		return nil, fmt.Errorf("drp provider requires a password for the specified user")
	}

	log.Printf("[DEBUG] Attempting to connect with credentials %+v", config)
	if err := config.validateAndConnect(); err != nil {
		return nil, err
	}

	info, err := config.session.Info()
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch info for %s", config.endpoint)
	}
	has_pool := false
	for _, f := range info.Features {
        if f == "embedded-pool" {
            has_pool = true
        }
    }
    if !has_pool {
    	return nil, fmt.Errorf("Pooling feature required.  Upgrade to v4.4 from %s", info.Version)	
    }
	
	log.Printf("[Info] Digital Rebar %+v", info.Version)

	return &config, nil
}
