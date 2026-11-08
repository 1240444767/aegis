/**
 * 把富文本编辑器产出的 HTML 转成 Markdown。
 *
 * 用在「富文本 → Markdown」切换格式时给作者一份起稿，不是通用转换器：只认 tiptap StarterKit
 * 加下划线、链接会产出的那十几个标签，加上净化白名单放行的文字颜色。认不出的标签保留文字、丢掉标签。
 * 转出来的 Markdown 交给服务端渲染时规则是「段内换行即换行」，所以 <br> 直接转成换行。
 */

const COLOR_STYLE = /(?:^|;)\s*color\s*:\s*([^;]+)/i;

function escapeInline(text: string): string {
  // 只转义会改变行内语义的字符；中文正文里很少出现，转义多了反而难读。
  return text.replace(/([\\`*_[\]])/g, "\\$1");
}

function inline(node: Node): string {
  if (node.nodeType === Node.TEXT_NODE) {
    return escapeInline((node.textContent ?? "").replace(/\s+/g, " "));
  }
  if (node.nodeType !== Node.ELEMENT_NODE) return "";
  const el = node as HTMLElement;
  const inner = () => Array.from(el.childNodes).map(inline).join("");
  switch (el.tagName.toLowerCase()) {
    case "strong":
    case "b": {
      const body = inner().trim();
      return body ? `**${body}**` : "";
    }
    case "em":
    case "i": {
      const body = inner().trim();
      return body ? `*${body}*` : "";
    }
    case "s":
    case "del":
    case "strike": {
      const body = inner().trim();
      return body ? `~~${body}~~` : "";
    }
    case "code":
      return `\`${(el.textContent ?? "").replace(/`/g, "ˋ")}\``;
    case "u":
    case "ins":
      return `<u>${inner()}</u>`;
    case "mark":
      return `<mark>${inner()}</mark>`;
    case "a": {
      const href = el.getAttribute("href") ?? "";
      const body = inner().trim() || href;
      return href ? `[${body}](${href})` : body;
    }
    case "br":
      return "\n";
    case "img": {
      const src = el.getAttribute("src") ?? "";
      return src ? `![${el.getAttribute("alt") ?? ""}](${src})` : "";
    }
    case "span":
    case "font": {
      const color = el.tagName.toLowerCase() === "font" ? el.getAttribute("color") : COLOR_STYLE.exec(el.getAttribute("style") ?? "")?.[1];
      const body = inner();
      return color ? `<span style="color:${color.trim()}">${body}</span>` : body;
    }
    default:
      return inner();
  }
}

function block(node: Node, depth = 0): string[] {
  if (node.nodeType === Node.TEXT_NODE) {
    const text = (node.textContent ?? "").trim();
    return text ? [escapeInline(text)] : [];
  }
  if (node.nodeType !== Node.ELEMENT_NODE) return [];
  const el = node as HTMLElement;
  const tag = el.tagName.toLowerCase();
  const heading = /^h([1-6])$/.exec(tag);
  if (heading) {
    return [`${"#".repeat(Number(heading[1]))} ${inline(el).trim()}`];
  }
  switch (tag) {
    case "p":
    case "figcaption": {
      const text = inline(el).trim();
      return text ? [text] : [];
    }
    case "blockquote":
      return [
        children(el, depth)
          .join("\n\n")
          .split("\n")
          .map((line) => (line ? `> ${line}` : ">"))
          .join("\n")
      ];
    case "pre":
      return ["```\n" + (el.textContent ?? "").replace(/\n$/, "") + "\n```"];
    case "hr":
      return ["---"];
    case "ul":
    case "ol":
      return [list(el, depth)];
    default:
      return children(el, depth);
  }
}

function children(el: HTMLElement, depth: number): string[] {
  return Array.from(el.childNodes).flatMap((child) => block(child, depth));
}

function list(el: HTMLElement, depth: number): string {
  const ordered = el.tagName.toLowerCase() === "ol";
  let index = Number(el.getAttribute("start") ?? "1") || 1;
  const indent = "  ".repeat(depth);
  const lines: string[] = [];
  for (const item of Array.from(el.children)) {
    if (item.tagName.toLowerCase() !== "li") continue;
    const marker = ordered ? `${index++}.` : "-";
    const text: string[] = [];
    const nested: string[] = [];
    for (const child of Array.from(item.childNodes)) {
      const name = child.nodeType === Node.ELEMENT_NODE ? (child as HTMLElement).tagName.toLowerCase() : "";
      if (name === "ul" || name === "ol") {
        nested.push(list(child as HTMLElement, depth + 1));
      } else if (name === "p") {
        text.push(inline(child).trim());
      } else {
        text.push(inline(child));
      }
    }
    lines.push(`${indent}${marker} ${text.join(" ").replace(/\s*\n\s*/g, " ").trim()}`);
    lines.push(...nested);
  }
  return lines.join("\n");
}

export function markdownFromHtml(html: string): string {
  if (!html.trim() || typeof DOMParser === "undefined") return "";
  const doc = new DOMParser().parseFromString(html, "text/html");
  return block(doc.body)
    .filter((part) => part.trim())
    .join("\n\n")
    .trim();
}
