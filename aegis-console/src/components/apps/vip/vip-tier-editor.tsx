"use client";

import { useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { FeatureChecklist } from "@/components/apps/vip/vip-shared";
import { useSaveAdminVipTierMutation } from "@/lib/vip-hooks";
import type { VipFeature, VipTier } from "@/lib/api/vip";

type Props = {
  appKey: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 为空即新建 */
  tier?: VipTier | null;
  features: VipFeature[];
};

type Form = { name: string; description: string; features: string[]; sortOrder: string };

function seed(tier?: VipTier | null): Form {
  return {
    name: tier?.name ?? "",
    description: tier?.description ?? "",
    features: tier?.features ?? [],
    sortOrder: String(tier?.sortOrder ?? 0)
  };
}

/**
 * 档位编辑抽屉。草稿按「正在编辑哪一个」绑定，与套餐编辑同一条约束（不用 effect 同步）。
 *
 * 档位的功能就是会员的权益：保存即对档位下全部在期会员生效，卖这个档位的套餐一起变。
 */
export function VipTierEditor({ appKey, open, onOpenChange, tier, features }: Props) {
  const scope = tier ? `tier:${tier.id}` : "new";
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const form = draft?.scope === scope ? draft.value : seed(tier);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  const saveMutation = useSaveAdminVipTierMutation(appKey);

  const submit = async () => {
    const name = form.name.trim();
    if (!name) {
      toast.error("档位名称不能为空");
      return;
    }
    try {
      await saveMutation.mutateAsync({
        id: tier?.id,
        name,
        description: form.description.trim(),
        features: form.features,
        sortOrder: Number(form.sortOrder) || 0
      });
      toast.success(tier ? "档位已更新" : "档位已创建");
      setDraft(null);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存档位失败");
    }
  };

  const affected = tier ? tier.members : 0;

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) setDraft(null);
        onOpenChange(next);
      }}
    >
      <SheetContent side="right" className="flex w-[92vw] max-w-xl flex-col gap-0 p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{tier ? "编辑档位" : "新建档位"}</SheetTitle>
          <SheetDescription>
            {tier && affected > 0 ? `修改功能会立即作用于 ${affected} 位在期会员` : "会员的权益由档位决定"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-5 px-6 py-5">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5 sm:col-span-2">
                <Label className="text-xs">名称</Label>
                <Input
                  value={form.name}
                  onChange={(event) => patch("name", event.target.value)}
                  placeholder="如：永久会员、高级会员"
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">排序（小的在前）</Label>
                <Input
                  inputMode="numeric"
                  value={form.sortOrder}
                  onChange={(event) => patch("sortOrder", event.target.value)}
                />
              </div>
              <div className="space-y-1.5 sm:col-span-2">
                <Label className="text-xs">描述</Label>
                <Textarea
                  rows={2}
                  value={form.description}
                  onChange={(event) => patch("description", event.target.value)}
                  placeholder="选填"
                />
              </div>
            </div>

            <div className="space-y-2">
              <div className="flex items-baseline justify-between gap-2">
                <Label className="text-xs">包含的功能</Label>
                <span className="text-[11px] text-muted-foreground">
                  已选 {form.features.length} / {features.length}
                </span>
              </div>
              <FeatureChecklist catalog={features} value={form.features} onChange={(next) => patch("features", next)} />
            </div>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={saveMutation.isPending}>
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
