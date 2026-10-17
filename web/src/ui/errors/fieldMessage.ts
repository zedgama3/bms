/** One input's message: `text` is the translated sentence; `reason` is set only in the
 *  `field.unknown` case and is rendered as `<span lang="en">` by FieldMessageText. */
export interface FieldMessage {
  readonly text: string;
  readonly reason?: string;
}
