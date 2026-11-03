package matter

// Matter IDs used by this adapter. Devices advertise their actual types,
// clusters, attributes, and feature maps; these constants name the subset we
// interpret. Source: https://github.com/project-chip/connectedhomeip/tree/master/data_model
const (
	clusterIdentify                 uint32 = 0x0003
	clusterGroups                   uint32 = 0x0004
	clusterOnOff                    uint32 = 0x0006
	clusterLevelControl             uint32 = 0x0008
	clusterDescriptor               uint32 = 0x001D
	clusterBasicInformation         uint32 = 0x0028
	clusterBridgedDeviceInformation uint32 = 0x0039
	clusterSwitch                   uint32 = 0x003B
	clusterDoorLock                 uint32 = 0x0101
	clusterWindowCovering           uint32 = 0x0102
	clusterThermostat               uint32 = 0x0201
	clusterColorControl             uint32 = 0x0300
	clusterTemperatureMeasurement   uint32 = 0x0402
	clusterOccupancySensing         uint32 = 0x0406
)

const (
	attributeOnOff                 uint32 = 0x0000
	attributeCurrentLevel          uint32 = 0x0000
	attributeMinLevel              uint32 = 0x0002
	attributeMaxLevel              uint32 = 0x0003
	attributeColorTemperature      uint32 = 0x0007
	attributeColorTempPhysicalMin  uint32 = 0x400B
	attributeColorTempPhysicalMax  uint32 = 0x400C
	attributeSwitchCurrentPosition uint32 = 0x0001
	attributeMeasuredTemperature   uint32 = 0x0000
	attributeOccupancy             uint32 = 0x0000
	attributeDeviceTypeList        uint32 = 0x0000
	attributeServerList            uint32 = 0x0001
	attributeClientList            uint32 = 0x0002
	attributeNodeLabel             uint32 = 0x0005
	attributeVendorName            uint32 = 0x0001
	attributeProductName           uint32 = 0x0003
	attributeBridgedNodeLabel      uint32 = 0x0005
	attributeAcceptedCommands      uint32 = 0xFFF9
	attributeEventList             uint32 = 0xFFFA
	attributeAttributeList         uint32 = 0xFFFB
	attributeFeatureMap            uint32 = 0xFFFC
)

const (
	commandMoveToLevelWithOnOff   uint32 = 0x0004
	commandMoveToColorTemperature uint32 = 0x000A
)

const (
	deviceTypeGenericSwitch      uint32 = 0x000F
	deviceTypeOnOffLight         uint32 = 0x0100
	deviceTypeDimmableLight      uint32 = 0x0101
	deviceTypeOnOffLightSwitch   uint32 = 0x0103
	deviceTypeDimmerSwitch       uint32 = 0x0104
	deviceTypeColorDimmerSwitch  uint32 = 0x0105
	deviceTypeOccupancySensor    uint32 = 0x0107
	deviceTypeColorTempLight     uint32 = 0x010C
	deviceTypeExtendedColorLight uint32 = 0x010D
	deviceTypeTemperatureSensor  uint32 = 0x0302
)

const (
	switchEventLatched            uint32 = 0x00
	switchEventInitialPress       uint32 = 0x01
	switchEventLongPress          uint32 = 0x02
	switchEventShortRelease       uint32 = 0x03
	switchEventLongRelease        uint32 = 0x04
	switchEventMultiPressOngoing  uint32 = 0x05
	switchEventMultiPressComplete uint32 = 0x06
)
