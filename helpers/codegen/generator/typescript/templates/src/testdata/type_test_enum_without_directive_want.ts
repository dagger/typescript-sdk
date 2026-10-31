
/**
 * Transport layer network protocol associated to a port.
 */
export enum NetworkProtocol {
  TCP = "TCP",
  UDP = "UDP",
}

/**
 * Utility function to convert a NetworkProtocol value to its name so
 * it can be uses as argument to call a exposed function.
 */
export function NetworkProtocolValueToName(value: NetworkProtocol): string {
  switch (value) {
    case NetworkProtocol.TCP:
      return "TCP"
    case NetworkProtocol.UDP:
      return "UDP"
    default:
      return value
  }
}

/**
 * Utility function to convert a NetworkProtocol name to its value so
 * it can be properly used inside the module runtime.
 */
export function NetworkProtocolNameToValue(name: string): NetworkProtocol {
  switch (name) {
    case "TCP":
      return NetworkProtocol.TCP
    case "UDP":
      return NetworkProtocol.UDP
    default:
      return name as NetworkProtocol
  }
}