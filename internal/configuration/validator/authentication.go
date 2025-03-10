package validator

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/authelia/authelia/internal/configuration/schema"
	"github.com/authelia/authelia/internal/utils"
)

//nolint:gocyclo // TODO: Consider refactoring/simplifying, time permitting.
func validateFileAuthenticationBackend(configuration *schema.FileAuthenticationBackendConfiguration, validator *schema.StructValidator) {
	if configuration.Path == "" {
		validator.Push(errors.New("Please provide a `path` for the users database in `authentication_backend`"))
	}

	if configuration.Password == nil {
		configuration.Password = &schema.DefaultPasswordConfiguration
	} else {
		if configuration.Password.Algorithm == "" {
			configuration.Password.Algorithm = schema.DefaultPasswordConfiguration.Algorithm
		} else {
			configuration.Password.Algorithm = strings.ToLower(configuration.Password.Algorithm)
			if configuration.Password.Algorithm != argon2id && configuration.Password.Algorithm != sha512 {
				validator.Push(fmt.Errorf("Unknown hashing algorithm supplied, valid values are argon2id and sha512, you configured '%s'", configuration.Password.Algorithm))
			}
		}

		// Iterations (time)
		if configuration.Password.Iterations == 0 {
			if configuration.Password.Algorithm == argon2id {
				configuration.Password.Iterations = schema.DefaultPasswordConfiguration.Iterations
			} else {
				configuration.Password.Iterations = schema.DefaultPasswordSHA512Configuration.Iterations
			}
		} else if configuration.Password.Iterations < 1 {
			validator.Push(fmt.Errorf("The number of iterations specified is invalid, must be 1 or more, you configured %d", configuration.Password.Iterations))
		}

		// Salt Length
		switch {
		case configuration.Password.SaltLength == 0:
			configuration.Password.SaltLength = schema.DefaultPasswordConfiguration.SaltLength
		case configuration.Password.SaltLength < 8:
			validator.Push(fmt.Errorf("The salt length must be 2 or more, you configured %d", configuration.Password.SaltLength))
		}

		if configuration.Password.Algorithm == argon2id {
			// Parallelism
			if configuration.Password.Parallelism == 0 {
				configuration.Password.Parallelism = schema.DefaultPasswordConfiguration.Parallelism
			} else if configuration.Password.Parallelism < 1 {
				validator.Push(fmt.Errorf("Parallelism for argon2id must be 1 or more, you configured %d", configuration.Password.Parallelism))
			}

			// Memory
			if configuration.Password.Memory == 0 {
				configuration.Password.Memory = schema.DefaultPasswordConfiguration.Memory
			} else if configuration.Password.Memory < configuration.Password.Parallelism*8 {
				validator.Push(fmt.Errorf("Memory for argon2id must be %d or more (parallelism * 8), you configured memory as %d and parallelism as %d", configuration.Password.Parallelism*8, configuration.Password.Memory, configuration.Password.Parallelism))
			}

			// Key Length
			if configuration.Password.KeyLength == 0 {
				configuration.Password.KeyLength = schema.DefaultPasswordConfiguration.KeyLength
			} else if configuration.Password.KeyLength < 16 {
				validator.Push(fmt.Errorf("Key length for argon2id must be 16, you configured %d", configuration.Password.KeyLength))
			}
		}
	}
}

// Wrapper for test purposes to exclude the hostname from the return.
func validateLdapURLSimple(ldapURL string, validator *schema.StructValidator) (finalURL string) {
	finalURL, _ = validateLdapURL(ldapURL, validator)

	return finalURL
}

func validateLdapURL(ldapURL string, validator *schema.StructValidator) (finalURL string, hostname string) {
	parsedURL, err := url.Parse(ldapURL)

	if err != nil {
		validator.Push(errors.New("Unable to parse URL to ldap server. The scheme is probably missing: ldap:// or ldaps://"))
		return "", ""
	}

	if !(parsedURL.Scheme == schemeLDAP || parsedURL.Scheme == schemeLDAPS) {
		validator.Push(errors.New("Unknown scheme for ldap url, should be ldap:// or ldaps://"))
		return "", ""
	}

	return parsedURL.String(), parsedURL.Hostname()
}

//nolint:gocyclo // TODO: Consider refactoring/simplifying, time permitting.
func validateLdapAuthenticationBackend(configuration *schema.LDAPAuthenticationBackendConfiguration, validator *schema.StructValidator) {
	if configuration.Implementation == "" {
		configuration.Implementation = schema.DefaultLDAPAuthenticationBackendConfiguration.Implementation
	}

	nilTLS := configuration.TLS == nil
	if nilTLS {
		configuration.TLS = schema.DefaultLDAPAuthenticationBackendConfiguration.TLS
	}

	// Deprecated. Maps deprecated values to the new ones. TODO: Remove in 4.28 (if block).
	if configuration.SkipVerify != nil {
		validator.PushWarning(errors.New("DEPRECATED: LDAP Auth Backend `skip_verify` option has been replaced by `authentication_backend.ldap.tls.skip_verify` (will be removed in 4.28.0)"))

		if nilTLS {
			configuration.TLS.SkipVerify = *configuration.SkipVerify
		}
	}

	// Deprecated. Maps deprecated values to the new ones. TODO: Remove in 4.28 (if block).
	if configuration.MinimumTLSVersion != "" {
		validator.PushWarning(errors.New("DEPRECATED: LDAP Auth Backend `minimum_tls_version` option has been replaced by `authentication_backend.ldap.tls.minimum_version` (will be removed in 4.28.0)"))

		if nilTLS {
			configuration.TLS.MinimumVersion = configuration.MinimumTLSVersion
		}
	}

	if configuration.TLS.MinimumVersion == "" {
		configuration.TLS.MinimumVersion = schema.DefaultLDAPAuthenticationBackendConfiguration.TLS.MinimumVersion
	}

	if _, err := utils.TLSStringToTLSConfigVersion(configuration.TLS.MinimumVersion); err != nil {
		validator.Push(fmt.Errorf("error occurred validating the LDAP minimum_tls_version key with value %s: %v", configuration.TLS.MinimumVersion, err))
	}

	switch configuration.Implementation {
	case schema.LDAPImplementationCustom:
		setDefaultImplementationCustomLdapAuthenticationBackend(configuration)
	case schema.LDAPImplementationActiveDirectory:
		setDefaultImplementationActiveDirectoryLdapAuthenticationBackend(configuration)
	default:
		validator.Push(fmt.Errorf("authentication backend ldap implementation must be blank or one of the following values `%s`, `%s`", schema.LDAPImplementationCustom, schema.LDAPImplementationActiveDirectory))
	}

	if configuration.URL == "" {
		validator.Push(errors.New("Please provide a URL to the LDAP server"))
	} else {
		ldapURL, serverName := validateLdapURL(configuration.URL, validator)

		configuration.URL = ldapURL

		if configuration.TLS.ServerName == "" {
			configuration.TLS.ServerName = serverName
		}
	}

	// TODO: see if it's possible to disable this check if disable_reset_password is set and when anonymous/user binding is supported (#101 and #387)
	if configuration.User == "" {
		validator.Push(errors.New("Please provide a user name to connect to the LDAP server"))
	}

	// TODO: see if it's possible to disable this check if disable_reset_password is set and when anonymous/user binding is supported (#101 and #387)
	if configuration.Password == "" {
		validator.Push(errors.New("Please provide a password to connect to the LDAP server"))
	}

	if configuration.BaseDN == "" {
		validator.Push(errors.New("Please provide a base DN to connect to the LDAP server"))
	}

	if configuration.UsersFilter == "" {
		validator.Push(errors.New("Please provide a users filter with `users_filter` attribute"))
	} else {
		if !strings.HasPrefix(configuration.UsersFilter, "(") || !strings.HasSuffix(configuration.UsersFilter, ")") {
			validator.Push(errors.New("The users filter should contain enclosing parenthesis. For instance {username_attribute}={input} should be ({username_attribute}={input})"))
		}

		if !strings.Contains(configuration.UsersFilter, "{username_attribute}") {
			validator.Push(errors.New("Unable to detect {username_attribute} placeholder in users_filter, your configuration is broken. " +
				"Please review configuration options listed at https://docs.authelia.com/configuration/authentication/ldap.html"))
		}

		// This test helps the user know that users_filter is broken after the breaking change induced by this commit.
		if !strings.Contains(configuration.UsersFilter, "{0}") && !strings.Contains(configuration.UsersFilter, "{input}") {
			validator.Push(errors.New("Unable to detect {input} placeholder in users_filter, your configuration might be broken. " +
				"Please review configuration options listed at https://docs.authelia.com/configuration/authentication/ldap.html"))
		}
	}

	if configuration.GroupsFilter == "" {
		validator.Push(errors.New("Please provide a groups filter with `groups_filter` attribute"))
	} else if !strings.HasPrefix(configuration.GroupsFilter, "(") || !strings.HasSuffix(configuration.GroupsFilter, ")") {
		validator.Push(errors.New("The groups filter should contain enclosing parenthesis. For instance cn={input} should be (cn={input})"))
	}
}

func setDefaultImplementationActiveDirectoryLdapAuthenticationBackend(configuration *schema.LDAPAuthenticationBackendConfiguration) {
	if configuration.UsersFilter == "" {
		configuration.UsersFilter = schema.DefaultLDAPAuthenticationBackendImplementationActiveDirectoryConfiguration.UsersFilter
	}

	if configuration.UsernameAttribute == "" {
		configuration.UsernameAttribute = schema.DefaultLDAPAuthenticationBackendImplementationActiveDirectoryConfiguration.UsernameAttribute
	}

	if configuration.DisplayNameAttribute == "" {
		configuration.DisplayNameAttribute = schema.DefaultLDAPAuthenticationBackendImplementationActiveDirectoryConfiguration.DisplayNameAttribute
	}

	if configuration.MailAttribute == "" {
		configuration.MailAttribute = schema.DefaultLDAPAuthenticationBackendImplementationActiveDirectoryConfiguration.MailAttribute
	}

	if configuration.GroupsFilter == "" {
		configuration.GroupsFilter = schema.DefaultLDAPAuthenticationBackendImplementationActiveDirectoryConfiguration.GroupsFilter
	}

	if configuration.GroupNameAttribute == "" {
		configuration.GroupNameAttribute = schema.DefaultLDAPAuthenticationBackendImplementationActiveDirectoryConfiguration.GroupNameAttribute
	}
}

func setDefaultImplementationCustomLdapAuthenticationBackend(configuration *schema.LDAPAuthenticationBackendConfiguration) {
	if configuration.UsernameAttribute == "" {
		configuration.UsernameAttribute = schema.DefaultLDAPAuthenticationBackendConfiguration.UsernameAttribute
	}

	if configuration.GroupNameAttribute == "" {
		configuration.GroupNameAttribute = schema.DefaultLDAPAuthenticationBackendConfiguration.GroupNameAttribute
	}

	if configuration.MailAttribute == "" {
		configuration.MailAttribute = schema.DefaultLDAPAuthenticationBackendConfiguration.MailAttribute
	}

	if configuration.DisplayNameAttribute == "" {
		configuration.DisplayNameAttribute = schema.DefaultLDAPAuthenticationBackendConfiguration.DisplayNameAttribute
	}
}

// ValidateAuthenticationBackend validates and update authentication backend configuration.
func ValidateAuthenticationBackend(configuration *schema.AuthenticationBackendConfiguration, validator *schema.StructValidator) {
	if configuration.Ldap == nil && configuration.File == nil {
		validator.Push(errors.New("Please provide `ldap` or `file` object in `authentication_backend`"))
	}

	if configuration.Ldap != nil && configuration.File != nil {
		validator.Push(errors.New("You cannot provide both `ldap` and `file` objects in `authentication_backend`"))
	}

	if configuration.File != nil {
		validateFileAuthenticationBackend(configuration.File, validator)
	} else if configuration.Ldap != nil {
		validateLdapAuthenticationBackend(configuration.Ldap, validator)
	}

	if configuration.RefreshInterval == "" {
		configuration.RefreshInterval = schema.RefreshIntervalDefault
	} else {
		_, err := utils.ParseDurationString(configuration.RefreshInterval)
		if err != nil && configuration.RefreshInterval != schema.ProfileRefreshDisabled && configuration.RefreshInterval != schema.ProfileRefreshAlways {
			validator.Push(fmt.Errorf("Auth Backend `refresh_interval` is configured to '%s' but it must be either a duration notation or one of 'disable', or 'always'. Error from parser: %s", configuration.RefreshInterval, err))
		}
	}
}
