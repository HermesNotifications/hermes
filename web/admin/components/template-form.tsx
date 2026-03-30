"use client";

import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter } from "next/navigation";
import { useTransition } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { templateSchema, type TemplateFormData } from "@/lib/schemas/template";
import { slugify } from "@/lib/utils";

interface TemplateFormProps {
  defaultValues?: Partial<TemplateFormData>;
  onSubmit: (data: TemplateFormData) => Promise<unknown>;
  isEdit?: boolean;
}

const CHANNELS = [
  { id: "email", label: "Email" },
  { id: "sms", label: "SMS" },
  { id: "inbox", label: "Inbox" },
] as const;

export function TemplateForm({ defaultValues, onSubmit, isEdit = false }: TemplateFormProps) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    control,
    formState: { errors },
  } = useForm<TemplateFormData>({
    resolver: zodResolver(templateSchema),
    defaultValues: {
      name: "",
      slug: "",
      subscriptionId: "",
      defaultChannels: [],
      emailSubject: "",
      emailBody: "",
      smsBody: "",
      inboxTitle: "",
      inboxBody: "",
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

  function handleFormSubmit(data: TemplateFormData) {
    startTransition(async () => {
      try {
        await onSubmit(data);
        toast.success(isEdit ? "Template updated" : "Template created");
        router.push("/templates");
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
          placeholder="Welcome Email"
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
          placeholder="welcome-email"
          {...register("slug")}
        />
        {errors.slug && (
          <p className="text-sm text-destructive">{errors.slug.message}</p>
        )}
        <p className="text-xs text-muted-foreground">
          Unique identifier used when sending notifications. Lowercase, alphanumeric, hyphens only.
        </p>
      </div>

      {/* Subscription ID (optional) */}
      <div className="space-y-1.5">
        <Label htmlFor="subscriptionId">Subscription ID <span className="font-normal text-muted-foreground">(optional)</span></Label>
        <Input
          id="subscriptionId"
          placeholder="sub_..."
          {...register("subscriptionId")}
        />
      </div>

      {/* Default Channels */}
      <div className="space-y-2">
        <Label>Default Channels</Label>
        <p className="text-xs text-muted-foreground">
          Select which channels to enable content fields for.
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

      {/* Email Section */}
      {selectedChannels.includes("email") && (
        <div className="space-y-4 rounded-lg border p-4">
          <h3 className="text-sm font-semibold">Email</h3>
          <div className="space-y-1.5">
            <Label htmlFor="emailSubject">Subject</Label>
            <Input
              id="emailSubject"
              placeholder="Welcome to {{app_name}}"
              {...register("emailSubject")}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="emailBody">Body</Label>
            <Textarea
              id="emailBody"
              placeholder="Hi {{user_name}}, welcome aboard!"
              rows={6}
              {...register("emailBody")}
            />
          </div>
        </div>
      )}

      {/* SMS Section */}
      {selectedChannels.includes("sms") && (
        <div className="space-y-4 rounded-lg border p-4">
          <h3 className="text-sm font-semibold">SMS</h3>
          <div className="space-y-1.5">
            <Label htmlFor="smsBody">
              Message
            </Label>
            <SmsBodyField register={register} watch={watch} />
          </div>
        </div>
      )}

      {/* Inbox Section */}
      {selectedChannels.includes("inbox") && (
        <div className="space-y-4 rounded-lg border p-4">
          <h3 className="text-sm font-semibold">Inbox</h3>
          <div className="space-y-1.5">
            <Label htmlFor="inboxTitle">Title</Label>
            <Input
              id="inboxTitle"
              placeholder="You have a new message"
              {...register("inboxTitle")}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="inboxBody">Body</Label>
            <Textarea
              id="inboxBody"
              placeholder="Hi {{user_name}}, check out your new message."
              rows={4}
              {...register("inboxBody")}
            />
          </div>
        </div>
      )}

      {/* Actions */}
      <div className="flex gap-2">
        <Button type="submit" disabled={isPending}>
          {isPending ? "Saving..." : isEdit ? "Update Template" : "Create Template"}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => router.push("/templates")}
          disabled={isPending}
        >
          Cancel
        </Button>
      </div>
    </form>
  );
}

function SmsBodyField({
  register,
  watch,
}: {
  register: ReturnType<typeof useForm<TemplateFormData>>["register"];
  watch: ReturnType<typeof useForm<TemplateFormData>>["watch"];
}) {
  const value = watch("smsBody") ?? "";
  return (
    <div className="space-y-1">
      <Textarea
        id="smsBody"
        placeholder="Hi {{user_name}}, your verification code is {{code}}"
        rows={3}
        {...register("smsBody")}
      />
      <p className="text-xs text-muted-foreground text-right">
        {value.length} / 160 characters
      </p>
    </div>
  );
}
