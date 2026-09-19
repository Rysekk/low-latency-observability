resource "oci_core_instance" "ubuntu_instance" {
    # Required
    availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
    compartment_id = oci_identity_compartment.tf-compartment.id
    shape = "VM.Standard.A1.Flex"
    shape_config {
        ocpus = "4"
        memory_in_gbs = "24"
    }
    source_details {
        source_id = data.oci_core_images.ubuntu_image.images[0].id
        source_type = "image"
    }

    # Optional
    display_name = "k3s-prod"
    create_vnic_details {
        assign_public_ip = true
        subnet_id = oci_core_subnet.subnet.id
    }
    metadata = {
        ssh_authorized_keys = file("${var.ssh_public_key_path}")
        user_data = "${base64encode(file("${path.module}/cloud-init.yaml"))}"
    } 
    preserve_boot_volume = false
}

output "instance_ip_public" {
  value     = oci_core_instance.ubuntu_instance.public_ip
}