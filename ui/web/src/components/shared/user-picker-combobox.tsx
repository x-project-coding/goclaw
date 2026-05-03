import { Combobox } from "@/components/ui/combobox";
import { useUserPicker } from "@/hooks/use-user-picker";

interface UserPickerComboboxProps {
  value: string;
  onChange: (value: string) => void;
  /** Fires only on dropdown item click or custom value commit (not keystrokes). */
  onSelect?: (value: string) => void;
  placeholder?: string;
  className?: string;
  /** Filter contacts by peer_kind: "direct" | "group" | undefined (all). */
  peerKind?: "direct" | "group";
  /** Filter contacts by source. Currently only "contact" is supported. */
  source?: "contact";
  /** Committed value shape. "user_id" (default) or "uuid". */
  valueMode?: "user_id" | "uuid";
  /** Allow typing custom values not in the list. Default true. */
  allowCustom?: boolean;
  /** Render dropdown into a portal container (useful inside dialogs). */
  portalContainer?: React.RefObject<HTMLElement | null>;
}

/**
 * User picker that searches channel_contacts.
 * - Shows 30 most recent results when opened (no typing needed)
 * - Debounced server-side search as user types
 * - Source badges: [telegram], [discord]
 */
export function UserPickerCombobox({
  value,
  onChange,
  onSelect,
  placeholder,
  className,
  peerKind,
  source,
  valueMode,
  allowCustom = true,
  portalContainer,
}: UserPickerComboboxProps) {
  const { options } = useUserPicker(value, peerKind, source, valueMode);

  return (
    <Combobox
      value={value}
      onChange={onChange}
      onSelect={onSelect}
      options={options}
      placeholder={placeholder}
      className={className}
      allowCustom={allowCustom}
      portalContainer={portalContainer}
    />
  );
}
