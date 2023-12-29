package configdomain

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/git-town/git-town/v11/src/messages"
)

type Runner interface {
	Query(executable string, args ...string) (string, error)
	Run(executable string, args ...string) error
}

// Access provides typesafe access to the Git configuration on disk.
type Access struct {
	Runner
}

// LoadGit provides the Git configuration from the given directory or the global one if the global flag is set.
func (self *Access) LoadCache(global bool) (SingleCache, PartialConfig, error) {
	cache := SingleCache{}
	config := EmptyPartialConfig()
	cmdArgs := []string{"config", "-lz"}
	if global {
		cmdArgs = append(cmdArgs, "--global")
	} else {
		cmdArgs = append(cmdArgs, "--local")
	}
	output, err := self.Runner.Query("git", cmdArgs...)
	if err != nil {
		return cache, config, nil //nolint:nilerr
	}
	if output == "" {
		return cache, config, nil
	}
	for _, line := range strings.Split(output, "\x00") {
		if len(line) == 0 {
			continue
		}
		parts := strings.SplitN(line, "\n", 2)
		key, value := parts[0], parts[1]
		configKey := ParseKey(key)
		if configKey == nil {
			continue
		}
		newKey, keyIsDeprecated := DeprecatedKeys[*configKey]
		if keyIsDeprecated {
			self.UpdateDeprecatedSetting(*configKey, newKey, value, global)
			configKey = &newKey
		}
		if key != KeyPerennialBranches.String() && value == "" {
			_ = self.RemoveLocalConfigValue(*configKey)
			fmt.Printf(messages.ConfigurationEmptyEntryDeleted, key)
			continue
		}
		cache[*configKey] = value
		err := config.Add(*configKey, value)
		if err != nil {
			return cache, config, err
		}
	}
	return cache, config, nil
}

func (self *Access) RemoveGlobalConfigValue(key Key) error {
	return self.Run("git", "config", "--global", "--unset", key.String())
}

// removeLocalConfigurationValue deletes the configuration value with the given key from the local Git Town configuration.
func (self *Access) RemoveLocalConfigValue(key Key) error {
	return self.Run("git", "config", "--unset", key.String())
}

// RemoveLocalGitConfiguration removes all Git Town configuration.
func (self *Access) RemoveLocalGitConfiguration(lineage Lineage) error {
	err := self.Run("git", "config", "--remove-section", "git-town")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 128 {
				// Git returns exit code 128 when trying to delete a non-existing config section.
				// This is not an error condition in this workflow so we can ignore it here.
				return nil
			}
		}
		return fmt.Errorf(messages.ConfigRemoveError, err)
	}
	for child := range lineage {
		key := fmt.Sprintf("git-town-branch.%s.parent", child)
		err = self.Run("git", "config", "--unset", key)
		if err != nil {
			return fmt.Errorf(messages.ConfigRemoveError, err)
		}
	}
	return nil
}

// SetGlobalConfigValue sets the given configuration setting in the global Git configuration.
func (self *Access) SetGlobalConfigValue(key Key, value string) error {
	return self.Run("git", "config", "--global", key.String(), value)
}

// SetLocalConfigValue sets the local configuration with the given key to the given value.
func (self *Access) SetLocalConfigValue(key Key, value string) error {
	return self.Run("git", "config", key.String(), value)
}

func (self *Access) UpdateDeprecatedGlobalSetting(oldKey, newKey Key, value string) {
	fmt.Printf("I found the deprecated global setting %q.\n", oldKey)
	fmt.Printf("I am upgrading this setting to the new format %q.\n", newKey)
	err := self.RemoveGlobalConfigValue(oldKey)
	if err != nil {
		fmt.Printf("ERROR: cannot remove global Git setting %q: %v", oldKey, err)
	}
	err = self.SetGlobalConfigValue(newKey, value)
	if err != nil {
		fmt.Printf("ERROR: cannot write global Git setting %q: %v", newKey, err)
	}
}

func (self *Access) UpdateDeprecatedLocalSetting(oldKey, newKey Key, value string) {
	fmt.Printf("I found the deprecated local setting %q.\n", oldKey)
	fmt.Printf("I am upgrading this setting to the new format %q.\n", newKey)
	err := self.RemoveLocalConfigValue(oldKey)
	if err != nil {
		fmt.Printf("ERROR: cannot remove local Git setting %q: %v", oldKey, err)
	}
	err = self.SetLocalConfigValue(newKey, value)
	if err != nil {
		fmt.Printf("ERROR: cannot write local Git setting %q: %v", newKey, err)
	}
}

func (self *Access) UpdateDeprecatedSetting(oldKey, newKey Key, value string, global bool) {
	if global {
		self.UpdateDeprecatedGlobalSetting(oldKey, newKey, value)
	} else {
		self.UpdateDeprecatedLocalSetting(oldKey, newKey, value)
	}
}
