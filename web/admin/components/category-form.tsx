"use client";

import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter } from "next/navigation";
import { useTransition } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { categorySchema, type CategoryFormData } from "@/lib/schemas/category";
import { slugify } from "@/lib/utils";

interface CategoryFormProps {
  defaultValues?: Partial<CategoryFormData>;
  onSubmit: (data: CategoryFormData) => Promise<unknown>;
  isEdit?: boolean;
}

const CHANNELS = [
  { id: "email", label: "Email" },
  { id: "sms", label: "SMS" },
  { id: "inbox", label: "Inbox" },
] as const;

export function CategoryForm({ defaultValues, onSubmit, isEdit = false }: CategoryFormProps) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    control,
    formState: { errors },
  } = useForm<CategoryFormData>({
    resolver: zodResolver(categorySchema),
    defaultValues: {
      name: "",
      slug: "",
      defaultChannels: [],
      defaultState: "on",
      sortOrder: 0,
      ...defaultValues,
    },
  });

  const selectedChannels = watch("defaultChannels") ?? [];

  function handleNameChange(e: React.ChangeEvent<HTMLInputElement>) {
    const name = e.target.value;
    setValue("name", name);
    if (!isEdit) {
      setValue("slug", slugify(name));
    }
  }

  function toggleChannel(channel: "email" | "sms" | "inbox", checked: boolean) {
    const current = selectedChannels;
    if (checked) {
      setValue("defaultChannels", [...current, channel]);
    } else {
      setValue("defaultChannels", current.filter((c) => c !== channel));
    }
  }

  function handleFormSubmit(data: CategoryFormData) {
    startTransition(async () => {
      try {
        await onSubmit(data);
        toast.success(isEdit ? "Category updated" : "Category created");
        router.push("/categories");
      } catch (err) {
        const message = err instanceof Error ? err.message : "Something went wrong";
        toast.error(message);
      }
    });
  }

  return (
    <form onSubmit={handleSubmit(handleFormSubmit)} className="space-y-6 max-w-2xl">
      {/* Name */}
      <div className="space-y-1.5">
        <Label htmlFor="name">Name</Label>
        <Input
          id="name"
          placeholder="Product Updates"
          {...register("name")}
          onChange={handleNameChange}
        />
        {errors.name && (
          <p className="text-sm text-destructive">{errors.name.message}</p>
        )}
      </div>

      {/* Slug */}
      <div className="space-y-1.5">
        <Label htmlFor="slug">Slug</Label>
        <Input
          id="slug"
          placeholder="product-updates"
          {...register("slug")}
        />
        {errors.slug && (
          <p className="text-sm text-destructive">{errors.slug.message}</p>
        )}
        <p className="text-xs text-muted-foreground">
          Unique identifier for this category. Lowercase, alphanumeric, hyphens only.
        </p>
      </div>

      {/* Default Channels */}
      <div className="space-y-2">
        <Label>Default Channels</Label>
        <p className="text-xs text-muted-foreground">
          Channels enabled by default for subscriptions in this category.
        </p>
        <div className="flex gap-4">
          {CHANNELS.map(({ id, label }) => (
            <Controller
              key={id}
              name="defaultChannels"
              control={control}
              render={() => (
                <label className="flex items-center gap-2 cursor-pointer">
                  <Checkbox
                    checked={selectedChannels.includes(id)}
                    onCheckedChange={(checked) => toggleChannel(id, !!checked)}
                  />
                  <span className="text-sm">{label}</span>
                </label>
              )}
            />
          ))}
        </div>
      </div>

      {/* Default State */}
      <div className="space-y-1.5">
        <Label htmlFor="defaultState">Default State</Label>
        <Controller
          name="defaultState"
          control={control}
          render={({ field }) => (
            <Select value={field.value} onValueChange={field.onChange}>
              <SelectTrigger id="defaultState" className="w-48">
                <SelectValue placeholder="Select state" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="on">On</SelectItem>
                <SelectItem value="off">Off</SelectItem>
                <SelectItem value="required">Required</SelectItem>
              </SelectContent>
            </Select>
          )}
        />
        {errors.defaultState && (
          <p className="text-sm text-destructive">{errors.defaultState.message}</p>
        )}
        <p className="text-xs text-muted-foreground">
          Whether users are subscribed by default. &ldquo;Required&rdquo; means users cannot unsubscribe.
        </p>
      </div>

      {/* Sort Order */}
      <div className="space-y-1.5">
        <Label htmlFor="sortOrder">Sort Order</Label>
        <Input
          id="sortOrder"
          type="number"
          min={0}
          className="w-32"
          {...register("sortOrder", { valueAsNumber: true })}
        />
        {errors.sortOrder && (
          <p className="text-sm text-destructive">{errors.sortOrder.message}</p>
        )}
        <p className="text-xs text-muted-foreground">
          Lower numbers appear first. Defaults to 0.
        </p>
      </div>

      {/* Actions */}
      <div className="flex gap-2">
        <Button type="submit" disabled={isPending}>
          {isPending ? "Saving..." : isEdit ? "Update Category" : "Create Category"}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => router.push("/categories")}
          disabled={isPending}
        >
          Cancel
        </Button>
      </div>
    </form>
  );
}
