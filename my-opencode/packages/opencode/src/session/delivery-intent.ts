/**
 * 任务是否需要"文件交付"上下文。
 *
 * 交付目录说明只在可能产出文件时才附到任务上：纯闲聊（"你好"、"叫爸爸"）不需要
 * 交付范围，每轮都带会让回复套上交付流程的框架，也会平白多出一段上下文。
 */
export type DeliveryInput = {
  parts?: Array<{ type: string; text?: string }>
  metadata?: Record<string, string>
}

/** 是否强制交付：命中后必须把最终文件放进交付目录，否则不算完成。 */
export function deliveryRequired(input: DeliveryInput) {
  const explicit = input.metadata?.delivery_required?.trim().toLowerCase()
  if (explicit === "true" || explicit === "1" || explicit === "yes") return true
  if (explicit === "false" || explicit === "0" || explicit === "no") return false

  const text = taskText(input)
  if (!text) return false

  if (EXPLICIT_DELIVERY.test(text)) return true
  if (SEND_ME_FILE.test(text) && FILE_OBJECT.test(text)) return true
  return CREATION_VERB.test(text) && FILE_OBJECT.test(text)
}

/** 是否可能产出需要交付的文件：只有命中才附交付目录说明。 */
export function mayProduceFiles(input: DeliveryInput) {
  const text = taskText(input)
  if (!text) return false
  if (CHAT_ONLY.test(text)) return false
  if (EXPLICIT_DELIVERY.test(text)) return true
  return FILE_OBJECT.test(text) || WORKSPACE_ACTION.test(text)
}

function taskText(input: DeliveryInput) {
  return (input.parts ?? [])
    .filter((part) => part.type === "text")
    .map((part) => part.text ?? "")
    .join("\n")
    .trim()
}

const EXPLICIT_DELIVERY = /(交付|可下载|下载(?:链接|文件)?|作为附件|附件|deliver(?:able|y)?|download(?:able)?|attachment)/i
const SEND_ME_FILE = /(发我|提供给我|给我一份|给我一个|send me|provide me|save as)/i
const FILE_OBJECT =
  /(文件|报告|文档|表格|电子表格|演示文稿|幻灯片|pdf|excel|csv|压缩包|安装包|图片|图像|视频|音频|源码包|file|report|document|spreadsheet|presentation|archive|package|image|video|audio)/i
const CREATION_VERB =
  /(生成|创建|制作|整理|编写|输出|保存|导出|打包|generate|create|make|write|produce|save|export|build|package)/i
const WORKSPACE_ACTION =
  /(修|改|写|生成|创建|制作|整理|编写|输出|保存|导出|打包|构建|编译|运行|跑|执行|分析|测试|调试|排查|定位|验证|核对|重构|优化|升级|迁移|实现|接入|替换|部署|安装|逆向|反编译|脱壳|注入|hook|patch|build|compile|run|analyze|test|debug|fix|deploy|install|reverse|decompile)/i
const CHAT_ONLY = /^(你好|您好|哈喽|在吗|在么|早|早上好|晚上好|谢谢|多谢|感谢|好的|收到|ok|okay|hi|hello|hey|thanks|thank you|test|测试)[\s。！!？?~～,，]*$/i

export * as DeliveryIntent from "./delivery-intent"
