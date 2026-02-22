import { useMemo } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { json } from "@codemirror/lang-json";
import { EditorView } from "@codemirror/view";

type Props = {
  value: string;
};

function normalizeCode(value: string): { text: string; isJSON: boolean } {
  const trimmed = value.trim();
  if (!trimmed) return { text: "", isJSON: false };

  try {
    const parsed = JSON.parse(trimmed);
    return { text: JSON.stringify(parsed, null, 2), isJSON: true };
  } catch {
    return { text: value, isJSON: false };
  }
}

export default function JsonCodeView({ value }: Props) {
  const code = useMemo(() => normalizeCode(value), [value]);

  return (
    <div style={{ marginTop: 8, border: "1px solid #e5e7eb", borderRadius: 10, overflow: "hidden" }}>
      <CodeMirror
        value={code.text}
        editable={false}
        readOnly
        height="auto"
        minHeight="72px"
        basicSetup={{
          foldGutter: true,
          lineNumbers: true,
          highlightActiveLine: false,
          highlightActiveLineGutter: false,
        }}
        extensions={code.isJSON ? [json(), EditorView.lineWrapping] : [EditorView.lineWrapping]}
      />
    </div>
  );
}
