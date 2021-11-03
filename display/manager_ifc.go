package display

import (
	"errors"
	"fmt"
	"os"

	"github.com/godbus/dbus"
	"pkg.deepin.io/lib/dbusutil"
	"pkg.deepin.io/lib/strv"
)

func (m *Manager) GetInterfaceName() string {
	return dbusInterface
}

func (m *Manager) applyChanges() error {
	if !m.HasChanged {
		return nil
	}
	err := m.apply(nil)
	return err
}

func (m *Manager) ApplyChanges() *dbus.Error {
	logger.Debug("dbus call ApplyChanges")
	err := m.applyChanges()
	return dbusutil.ToError(err)
}

func (m *Manager) ResetChanges() *dbus.Error {
	logger.Debug("dbus call ResetChanges")
	if !m.HasChanged {
		return nil
	}

	for _, monitor := range m.monitorMap {
		monitor.resetChanges()
	}

	err := m.apply(nil)
	if err != nil {
		return dbusutil.ToError(err)
	}

	m.setPropHasChanged(false)
	return nil
}

func (m *Manager) SwitchMode(mode byte, name string) *dbus.Error {
	logger.Debug("dbus call SwitchMode", mode, name)
	err := m.switchMode(mode, name)
	return dbusutil.ToError(err)
}

func (m *Manager) Save() *dbus.Error {
	logger.Debug("dbus call Save")
	err := m.save()
	return dbusutil.ToError(err)
}

func (m *Manager) AssociateTouch(outputName, touchSerial string) *dbus.Error {
	var touchUUID string
	for _, v := range m.Touchscreens {
		if v.Serial == touchSerial {
			touchUUID = v.uuid
			break
		}
	}

	if touchUUID == "" {
		return dbusutil.ToError(errors.New("touchscreen not exists"))
	}

	monitor := m.getConnectedMonitors().GetByName(outputName)
	if monitor == nil {
		return dbusutil.ToError(errors.New("monitor not exists"))
	}

	err := m.associateTouch(monitor, touchUUID, false)
	return dbusutil.ToError(err)
}

// ChangeBrightness 通过键盘控制所有显示器一起亮度加或减，保存配置。
func (m *Manager) ChangeBrightness(raised bool) *dbus.Error {
	logger.Debug("dbus call ChangeBrightness", raised)
	err := m.changeBrightness(raised)
	return dbusutil.ToError(err)
}

func (m *Manager) GetBrightness() (map[string]float64, *dbus.Error) {
	m.PropsMu.RLock()
	defer m.PropsMu.RUnlock()
	return m.Brightness, nil
}

func (m *Manager) ListOutputNames() ([]string, *dbus.Error) {
	logger.Debug("dbus call ListOutputNames")
	var names []string
	monitors := m.getConnectedMonitors()
	for _, monitor := range monitors {
		names = append(names, monitor.Name)
	}
	return names, nil
}

func (m *Manager) ListOutputsCommonModes() ([]ModeInfo, *dbus.Error) {
	logger.Debug("dbus call ListOutputsCommonModes")
	monitors := m.getConnectedMonitors()
	if len(monitors) == 0 {
		return nil, nil
	}

	commonSizes := getMonitorsCommonSizes(monitors)
	result := make([]ModeInfo, len(commonSizes))
	for i, size := range commonSizes {
		result[i] = getFirstModeBySize(monitors[0].Modes, size.width, size.height)
	}
	return result, nil
}

func (m *Manager) ModifyConfigName(name, newName string) *dbus.Error {
	err := m.modifyConfigName(name, newName)
	return dbusutil.ToError(err)
}

func (m *Manager) DeleteCustomMode(name string) *dbus.Error {
	return dbusutil.ToError(errors.New("obsoleted interface"))
}

// RefreshBrightness 重置亮度，主要被 session/power 模块调用。从配置恢复亮度。
func (m *Manager) RefreshBrightness() *dbus.Error {
	logger.Debug("dbus call RefreshBrightness")
	configs := m.getSuitableSysMonitorConfigs(m.DisplayMode)
	for _, config := range configs {
		if config.Enabled {
			err := m.setBrightness(config.Name, config.Brightness)
			if err != nil {
				logger.Warning(err)
			}
		}
	}
	m.syncPropBrightness()
	return nil
}

func (m *Manager) Reset() *dbus.Error {
	// TODO
	return nil
}

// SetAndSaveBrightness 设置并保持亮度
func (m *Manager) SetAndSaveBrightness(outputName string, value float64) *dbus.Error {
	logger.Debug("dbus call SetAndSaveBrightness", outputName, value)
	can, _ := m.CanSetBrightness(outputName)
	if !can {
		return dbusutil.ToError(fmt.Errorf("the port %s cannot set brightness", outputName))
	}
	err := m.setBrightnessAndSync(outputName, value)
	if err != nil {
		logger.Warning(err)
		return dbusutil.ToError(err)
	}

	err = m.saveBrightnessInCfg(map[string]float64{
		outputName: value,
	})
	if err != nil {
		logger.Warning(err)
		return dbusutil.ToError(err)
	}
	return nil
}

// SetBrightness 设置亮度但是不保存, 主要被 session/power 模块调用。
func (m *Manager) SetBrightness(outputName string, value float64) *dbus.Error {
	logger.Debug("dbus call SetBrightness", outputName, value)
	can, _ := m.CanSetBrightness(outputName)
	if !can {
		return dbusutil.ToError(fmt.Errorf("the port %s cannot set brightness", outputName))
	}

	err := m.setBrightnessAndSync(outputName, value)
	if err != nil {
		logger.Warning(err)
		return dbusutil.ToError(err)
	}
	return nil
}

func (m *Manager) SetPrimary(outputName string) *dbus.Error {
	logger.Debug("dbus call SetPrimary", outputName)
	err := m.setPrimary(outputName)
	return dbusutil.ToError(err)
}

func (m *Manager) CanRotate() (bool, *dbus.Error) {
	if os.Getenv("DEEPIN_DISPLAY_DISABLE_ROTATE") == "1" {
		return false, nil
	}
	return true, nil
}

func (m *Manager) CanSetBrightness(outputName string) (bool, *dbus.Error) {
	if outputName == "" {
		return false, dbusutil.ToError(errors.New("monitor Name is err"))
	}

	//如果是龙芯集显，且不是内置显示器，则不支持调节亮度
	if os.Getenv("CAN_SET_BRIGHTNESS") == "N" {
		if m.builtinMonitor == nil || m.builtinMonitor.Name != outputName {
			return false, nil
		}
	}
	return true, nil
}

func (m *Manager) getBuiltinMonitor() *Monitor {
	m.builtinMonitorMu.Lock()
	defer m.builtinMonitorMu.Unlock()
	return m.builtinMonitor
}

func (m *Manager) GetBuiltinMonitor() (string, dbus.ObjectPath, *dbus.Error) {
	builtinMonitor := m.getBuiltinMonitor()
	if builtinMonitor == nil {
		return "", "/", nil
	}

	m.monitorMapMu.Lock()
	_, ok := m.monitorMap[builtinMonitor.ID]
	m.monitorMapMu.Unlock()
	if !ok {
		return "", "/", dbusutil.ToError(fmt.Errorf("not found monitor %d", builtinMonitor.ID))
	}

	return builtinMonitor.Name, builtinMonitor.getPath(), nil
}

func (m *Manager) SetMethodAdjustCCT(adjustMethod int32) *dbus.Error {
	err := m.setColorTempMode(adjustMethod)
	return dbusutil.ToError(err)
}

func (m *Manager) SetColorTemperature(value int32) *dbus.Error {
	err := m.setColorTempValue(value)
	return dbusutil.ToError(err)
}

func (m *Manager) GetRealDisplayMode() (uint8, *dbus.Error) {
	monitors := m.getConnectedMonitors()

	mode := DisplayModeUnknown
	var pairs strv.Strv
	for _, m := range monitors {
		if !m.Enabled {
			continue
		}

		pair := fmt.Sprintf("%d,%d", m.X, m.Y)

		// 左上角座标相同，是复制
		if pairs.Contains(pair) {
			mode = DisplayModeMirror
		}

		pairs = append(pairs, pair)
	}

	if mode == DisplayModeUnknown && len(pairs) != 0 {
		if len(pairs) == 1 {
			mode = DisplayModeOnlyOne
		} else {
			mode = DisplayModeExtend
		}
	}

	return mode, nil
}

//func (m *Manager) SetConfig(cfgStr string) *dbus.Error {
//	err := m.setConfig(cfgStr)
//	return dbusutil.ToError(err)
//}
//
//// 应用系统级的配置
//func (m *Manager) setConfig(cfgStr string) error {
//	var cfg SysRootConfig
//	err := jsonUnmarshal(cfgStr, &cfg)
//	if err != nil {
//		return err
//	}
//
//	m.sysConfig = cfg
//	return nil
//}
